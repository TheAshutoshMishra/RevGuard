package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// ReconciliationOutcome describes what Reconcile did.
type ReconciliationOutcome struct {
	Case   *domain.RecoveryCase
	Action *domain.RecoveryAction
	// Applied is true only when this call actually transitioned the case
	// out of VERIFYING (to SUCCESS, FAILED, or UNKNOWN). False means a
	// safe no-op: the case was already resolved, the provider's answer
	// was inconclusive (PENDING or an ambiguous error), or a racing call
	// already won.
	Applied bool
}

// ReconciliationEngine establishes financial truth for a VERIFYING
// RecoveryCase, either from evidence RevGuard already has (a RecoveryAction
// that itself definitively FAILED at execution time) or by asking the
// provider's own authoritative state what actually happened — the
// explicit, on-demand counterpart to WebhookProcessor's passive ingestion.
// Like WebhookProcessor, it shares applyFinancialOutcome so both paths
// reconcile through identical, once-only, monotonic logic. See
// docs/architecture/webhooks-reconciliation.md.
//
// Reconcile never executes a payment, never creates a payment link, and
// never causes any new financial side effect — it only ever reads
// (PaymentReconciler.Reconcile is read-only by construction) and records
// what it learns.
type ReconciliationEngine struct {
	pool       *pgxpool.Pool
	reconciler PaymentReconciler
	logger     *slog.Logger
}

func NewReconciliationEngine(pool *pgxpool.Pool, reconciler PaymentReconciler, logger *slog.Logger) *ReconciliationEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReconciliationEngine{pool: pool, reconciler: reconciler, logger: logger}
}

// Reconcile establishes financial truth for recoveryCaseID's single
// RecoveryAction (Milestone 6 never creates more than one per case).
//
// Decision order:
//  1. Case must currently be VERIFYING (ErrRecoveryCaseNotVerifying) and
//     must have a RecoveryAction (ErrNoRecoveryActionForCase) — anything
//     else is a genuine caller/structural error.
//  2. If the RecoveryAction itself already definitively FAILED at
//     execution time (Milestone 6), no external call is needed at all —
//     that is already sufficient evidence that this attempt recovered
//     nothing, and it is propagated to the case as FAILED directly.
//  3. If the action carries no ProviderReference at all (only possible
//     when its own execution outcome was UNKNOWN — SUCCEEDED always
//     carries one, FAILED is handled above), there is nothing any
//     reconciliation attempt, now or later, could ever look up. The case
//     is moved to UNKNOWN once — terminal for automation, awaiting
//     manual review — rather than left in VERIFYING forever with no path
//     forward.
//  4. Otherwise the provider is asked. CAPTURED/FAILED (definitive)
//     applies the same way a webhook would. PENDING, or the provider
//     reporting an ambiguous failure (timeout/transport), leaves the
//     case exactly where it was — safe to call again later, never
//     guessed, never automatically retried. The provider affirmatively
//     reporting no record of the reference (ErrReconciliationReferenceNotFound)
//     is treated like case 3: not ambiguous, but a dead end for this
//     reference — resolved to UNKNOWN.
func (e *ReconciliationEngine) Reconcile(ctx context.Context, recoveryCaseID uuid.UUID) (*ReconciliationOutcome, error) {
	recoveryCase, err := e.loadCase(ctx, recoveryCaseID)
	if err != nil {
		return nil, err
	}
	if recoveryCase.Status != domain.RecoveryCaseStatusVerifying {
		return nil, fmt.Errorf("%w: case %s is %q", ErrRecoveryCaseNotVerifying, recoveryCaseID, recoveryCase.Status)
	}

	action, err := e.loadAction(ctx, recoveryCaseID)
	if err != nil {
		return nil, err
	}

	if action.Status == domain.RecoveryActionStatusFailed {
		amount, _ := computeRecoveredAmount(domain.RecoveryOutcomeStatusFailed, 0, "", recoveryCase.RevenueAtRisk.Currency)
		return e.resolve(ctx, recoveryCase, action,
			domain.RecoveryCaseStatusFailed, domain.RecoveryOutcomeStatusFailed, domain.RecoveryActionStatusFailed,
			amount, "", time.Now().UTC(),
			map[string]any{"reason": "execution_attempt_definitively_failed", "error_code": action.ErrorCode})
	}

	if action.ProviderReference == "" {
		amount, _ := computeRecoveredAmount(domain.RecoveryOutcomeStatusUnknown, 0, "", recoveryCase.RevenueAtRisk.Currency)
		return e.resolve(ctx, recoveryCase, action,
			domain.RecoveryCaseStatusUnknown, domain.RecoveryOutcomeStatusUnknown, domain.RecoveryActionStatusUnknown,
			amount, "", time.Now().UTC(),
			map[string]any{"reason": ErrNoProviderReferenceToReconcile.Error()})
	}

	e.logger.Info("reconciliation lookup started",
		"recovery_case_id", recoveryCaseID, "recovery_action_id", action.ID,
		"provider", action.Provider, "provider_reference", action.ProviderReference)
	result, reconcileErr := e.reconciler.Reconcile(ctx, ReconciliationRequest{
		Provider: action.Provider, ProviderReference: action.ProviderReference,
	})

	switch {
	case errors.Is(reconcileErr, ErrReconciliationReferenceNotFound):
		amount, _ := computeRecoveredAmount(domain.RecoveryOutcomeStatusUnknown, 0, "", recoveryCase.RevenueAtRisk.Currency)
		return e.resolve(ctx, recoveryCase, action,
			domain.RecoveryCaseStatusUnknown, domain.RecoveryOutcomeStatusUnknown, domain.RecoveryActionStatusUnknown,
			amount, "", time.Now().UTC(),
			map[string]any{"reason": reconcileErr.Error()})

	case reconcileErr != nil:
		// Ambiguous: timeout, transport failure, or any other condition
		// where the provider's real answer is unknown, not "no." Stay in
		// VERIFYING, never guess, never auto-retry.
		e.logger.Info("reconciliation inconclusive; case unchanged",
			"recovery_case_id", recoveryCaseID, "recovery_action_id", action.ID, "error", reconcileErr.Error())
		if err := e.auditNoOp(ctx, recoveryCaseID, "reconciliation.inconclusive", reconcileErr.Error()); err != nil {
			return nil, err
		}
		return &ReconciliationOutcome{Case: recoveryCase, Action: action}, nil

	case result.Status == domain.ProviderEventStatusPending:
		if err := e.auditNoOp(ctx, recoveryCaseID, "reconciliation.pending", ""); err != nil {
			return nil, err
		}
		return &ReconciliationOutcome{Case: recoveryCase, Action: action}, nil
	}

	targetCaseStatus, outcomeStatus, resolvedActionStatus, err := mapProviderEventToOutcome(result.Status)
	if err != nil {
		return nil, fmt.Errorf("service: unreachable: %w", err)
	}
	recoveredAmount, ok := computeRecoveredAmount(outcomeStatus, result.AmountMinorUnits, domain.Currency(result.Currency), recoveryCase.RevenueAtRisk.Currency)
	if !ok {
		// A CAPTURED result with no definitive amount/currency is not
		// strong enough evidence to fabricate a SUCCESS outcome.
		e.logger.Warn("reconciliation reported captured but carried no definitive amount/currency; not guessed into SUCCESS",
			"recovery_case_id", recoveryCaseID, "recovery_action_id", action.ID)
		if err := e.auditNoOp(ctx, recoveryCaseID, "reconciliation.ignored_insufficient_evidence", ""); err != nil {
			return nil, err
		}
		return &ReconciliationOutcome{Case: recoveryCase, Action: action}, nil
	}

	return e.resolve(ctx, recoveryCase, action, targetCaseStatus, outcomeStatus, resolvedActionStatus,
		recoveredAmount, result.ProviderPaymentReference, result.OccurredAt,
		map[string]any{"provider_payment_reference": result.ProviderPaymentReference})
}

func (e *ReconciliationEngine) loadCase(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.RecoveryCase, error) {
	recoveryCase, err := repository.NewPostgresRecoveryCaseRepository(e.pool).GetByID(ctx, recoveryCaseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrRecoveryCaseNotFound, recoveryCaseID)
		}
		return nil, fmt.Errorf("service: load recovery case: %w", err)
	}
	return recoveryCase, nil
}

// loadAction returns recoveryCaseID's single RecoveryAction. Called only
// once the case is already confirmed to be VERIFYING, at which point the
// absence of any RecoveryAction is a genuine structural anomaly
// (ErrNoRecoveryActionForCase) — Milestone 6 never reaches VERIFYING
// without creating one.
func (e *ReconciliationEngine) loadAction(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.RecoveryAction, error) {
	actions, err := repository.NewPostgresRecoveryActionRepository(e.pool).ListByRecoveryCaseID(ctx, recoveryCaseID)
	if err != nil {
		return nil, fmt.Errorf("service: list recovery actions: %w", err)
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoRecoveryActionForCase, recoveryCaseID)
	}
	// Exactly one RecoveryAction per case in this milestone's scope
	// (Milestone 6 never creates a second); pick the most recently
	// requested defensively in case that ever changes.
	return actions[len(actions)-1], nil
}

// resolve is the single path that ever calls applyFinancialOutcome from
// ReconciliationEngine — used for a definitive provider CAPTURED/FAILED
// result, an already-known execution-time FAILED, and both dead-end
// UNKNOWN cases (no reference at all, or provider reports no record of
// the reference).
func (e *ReconciliationEngine) resolve(
	ctx context.Context, recoveryCase *domain.RecoveryCase, action *domain.RecoveryAction,
	targetCaseStatus domain.RecoveryCaseStatus, outcomeStatus domain.RecoveryOutcomeStatus,
	resolvedActionStatus domain.RecoveryActionStatus, recoveredAmount domain.Money,
	externalReference string, observedAt time.Time, metadata map[string]any,
) (*ReconciliationOutcome, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	result, err := applyFinancialOutcome(ctx, tx, e.logger, financialOutcomeInput{
		RecoveryCaseID: recoveryCase.ID, RecoveryActionID: action.ID,
		TargetCaseStatus: targetCaseStatus, OutcomeStatus: outcomeStatus,
		RecoveredAmount: recoveredAmount, ExternalReference: externalReference,
		Provider: action.Provider, Source: domain.RecoveryOutcomeSourceReconciliation,
		ObservedAt: observedAt, ActorType: domain.AuditActorTypeSystem,
		ResolveUnknownAction: true, ResolvedActionStatus: resolvedActionStatus,
		Metadata: metadata,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	if result.Applied {
		recoveryCase.Status = targetCaseStatus
		recoveryCase.UpdatedAt = time.Now().UTC()
	}
	return &ReconciliationOutcome{Case: recoveryCase, Action: action, Applied: result.Applied}, nil
}

func (e *ReconciliationEngine) auditNoOp(ctx context.Context, recoveryCaseID uuid.UUID, eventType, detail string) error {
	auditRepo := repository.NewPostgresAuditEventRepository(e.pool)
	meta := map[string]any{}
	if detail != "" {
		meta["detail"] = detail
	}
	return auditRepo.Create(ctx, &domain.AuditEvent{
		ID: uuid.New(), RecoveryCaseID: recoveryCaseID, EventType: eventType,
		ActorType: domain.AuditActorTypeSystem, Metadata: auditJSON(meta), CreatedAt: time.Now().UTC(),
	})
}
