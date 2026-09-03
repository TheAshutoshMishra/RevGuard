package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// financialOutcomeInput is everything applyFinancialOutcome needs to
// establish trusted financial truth for a RecoveryCase. Every field is
// derived from durable state or authoritative provider evidence — never
// from a client request.
type financialOutcomeInput struct {
	RecoveryCaseID   uuid.UUID
	RecoveryActionID uuid.UUID

	// TargetCaseStatus is SUCCESS or FAILED — the only two values
	// applyFinancialOutcome ever transitions a case to.
	TargetCaseStatus domain.RecoveryCaseStatus
	// OutcomeStatus mirrors TargetCaseStatus but in
	// domain.RecoveryOutcomeStatus's own vocabulary (kept distinct on
	// purpose — see domain.RecoveryOutcome's doc comment).
	OutcomeStatus domain.RecoveryOutcomeStatus

	// RecoveredAmount is the provider-confirmed captured amount for
	// SUCCESS, or a zero-value Money for FAILED. Never the original
	// payment amount, an economic estimate, or any RevGuard-internal
	// figure — see docs/architecture/webhooks-reconciliation.md.
	RecoveredAmount domain.Money

	ExternalReference      string
	Provider               string
	Source                 domain.RecoveryOutcomeSource
	ProviderWebhookEventID *uuid.UUID
	ObservedAt             time.Time
	ActorType              domain.AuditActorType

	// ResolveUnknownAction, when true, also resolves the RecoveryAction's
	// own Status from UNKNOWN to ResolvedActionStatus — but only if it is
	// still UNKNOWN (guarded update). This describes the execution
	// request's own now-known outcome; it is not itself financial truth,
	// which lives exclusively in RecoveryOutcome/RecoveryCase.Status.
	ResolveUnknownAction bool
	ResolvedActionStatus domain.RecoveryActionStatus

	Metadata map[string]any
}

type financialOutcomeResult struct {
	// Applied is true only if this call actually performed the
	// VERIFYING -> target transition. False means the case was no longer
	// VERIFYING (already resolved, or racing with a concurrent winner) —
	// a safe, audited no-op, never an error.
	Applied bool
	Outcome *domain.RecoveryOutcome
}

// applyFinancialOutcome is the single place Milestone 7 ever transitions a
// RecoveryCase out of VERIFYING. Both WebhookProcessor and
// ReconciliationEngine call this — sharing it is what guarantees webhook
// and reconciliation evidence are reconciled through identical,
// once-only, monotonic logic rather than two subtly different code paths.
//
// It runs entirely within the caller's existing transaction (tx) — no
// external call happens here, so there is no reason for this to open its
// own transaction; the caller decides the transaction boundary (see
// WebhookProcessor.Process, which never needs a network call and does
// everything in one transaction, and ReconciliationEngine.Reconcile,
// where this runs as Phase 3 after Phase 2's external call has already
// completed with no transaction open).
//
// The guarded RecoveryCase.Status UPDATE (VERIFYING -> target) is the
// PRIMARY concurrency gate, attempted FIRST, before anything else is
// written. PostgreSQL serializes concurrent UPDATEs to the same row via
// row-level locking: the loser's UPDATE simply affects 0 rows once it
// finally runs (the winner's committed change means WHERE status =
// VERIFYING no longer matches), which is indistinguishable from "this
// case was already resolved" — exactly the safe no-op this function
// returns for both cases. This is what makes terminal financial truth
// monotonic: at most one call to this function, ever, can succeed for a
// given case, regardless of how many webhooks or reconciliation attempts
// race for it or arrive out of order. See
// docs/architecture/webhooks-reconciliation.md.
func applyFinancialOutcome(ctx context.Context, tx pgx.Tx, logger *slog.Logger, input financialOutcomeInput) (*financialOutcomeResult, error) {
	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	outcomeRepo := repository.NewPostgresRecoveryOutcomeRepository(tx)
	actionRepo := repository.NewPostgresRecoveryActionRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)
	now := time.Now().UTC()

	if err := ValidateTransition(domain.RecoveryCaseStatusVerifying, input.TargetCaseStatus); err != nil {
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}

	if err := caseRepo.UpdateStatus(ctx, input.RecoveryCaseID, domain.RecoveryCaseStatusVerifying, input.TargetCaseStatus, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			if auditErr := auditRepo.Create(ctx, &domain.AuditEvent{
				ID:             uuid.New(),
				RecoveryCaseID: input.RecoveryCaseID,
				EventType:      "recovery_outcome.rejected",
				ActorType:      input.ActorType,
				Metadata: auditJSON(map[string]any{
					"reason":             "case_not_verifying_or_already_resolved",
					"attempted_status":   string(input.TargetCaseStatus),
					"recovery_action_id": input.RecoveryActionID,
					"source":             string(input.Source),
				}),
				CreatedAt: now,
			}); auditErr != nil {
				return nil, fmt.Errorf("service: audit rejected outcome: %w", auditErr)
			}
			logger.Info("financial outcome rejected; case already resolved or not verifying",
				"recovery_case_id", input.RecoveryCaseID, "attempted_status", string(input.TargetCaseStatus))
			return &financialOutcomeResult{Applied: false}, nil
		}
		return nil, fmt.Errorf("service: transition recovery case: %w", err)
	}

	outcome := &domain.RecoveryOutcome{
		ID:                     uuid.New(),
		RecoveryCaseID:         input.RecoveryCaseID,
		RecoveryActionID:       input.RecoveryActionID,
		Status:                 input.OutcomeStatus,
		RecoveredAmount:        input.RecoveredAmount,
		ExternalReference:      input.ExternalReference,
		ObservedAt:             input.ObservedAt,
		CreatedAt:              now,
		Provider:               input.Provider,
		Source:                 input.Source,
		ProviderWebhookEventID: input.ProviderWebhookEventID,
		Metadata:               auditJSON(input.Metadata),
	}
	created, err := outcomeRepo.TryCreate(ctx, outcome)
	if err != nil {
		return nil, fmt.Errorf("service: persist recovery outcome: %w", err)
	}
	if !created {
		// Should not be reachable given the case-status gate above
		// already serialized this case to exactly one winner, but handled
		// defensively rather than assumed: reload whatever outcome does
		// exist rather than erroring.
		existing, err := outcomeRepo.GetByRecoveryActionID(ctx, input.RecoveryActionID)
		if err != nil {
			return nil, fmt.Errorf("service: reload recovery outcome after unexpected conflict: %w", err)
		}
		outcome = existing
		logger.Warn("recovery outcome already existed despite winning the case transition; anomalous but non-fatal",
			"recovery_case_id", input.RecoveryCaseID, "recovery_action_id", input.RecoveryActionID)
	}

	if input.ResolveUnknownAction {
		if err := actionRepo.UpdateExecutionResult(
			ctx, input.RecoveryActionID, domain.RecoveryActionStatusUnknown, input.ResolvedActionStatus,
			now, "", "", auditJSON(map[string]any{"resolved_by": string(input.Source), "recovery_outcome_id": outcome.ID}),
		); err != nil && !errors.Is(err, repository.ErrNotFound) {
			// ErrNotFound here just means the action wasn't UNKNOWN
			// (already SUCCEEDED/FAILED from Milestone 6) — a legitimate,
			// common no-op, not an error.
			return nil, fmt.Errorf("service: resolve recovery action status: %w", err)
		}
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: input.RecoveryCaseID,
		EventType:      "recovery_outcome.recorded",
		ActorType:      input.ActorType,
		Metadata: auditJSON(map[string]any{
			"recovery_action_id":           input.RecoveryActionID,
			"recovery_outcome_id":          outcome.ID,
			"status":                       string(input.OutcomeStatus),
			"source":                       string(input.Source),
			"provider":                     input.Provider,
			"recovered_amount_minor_units": input.RecoveredAmount.MinorUnits,
			"currency":                     string(input.RecoveredAmount.Currency),
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit recorded outcome: %w", err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: input.RecoveryCaseID,
		EventType:      "recovery_case.transitioned",
		ActorType:      input.ActorType,
		Metadata: auditJSON(map[string]any{
			"from":   string(domain.RecoveryCaseStatusVerifying),
			"to":     string(input.TargetCaseStatus),
			"reason": "financial_outcome_confirmed",
			"source": string(input.Source),
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit case transition: %w", err)
	}

	logger.Info("recovery case financial outcome established",
		"recovery_case_id", input.RecoveryCaseID, "recovery_action_id", input.RecoveryActionID,
		"status", string(input.OutcomeStatus), "source", string(input.Source),
		"recovered_amount_minor_units", input.RecoveredAmount.MinorUnits)

	return &financialOutcomeResult{Applied: true, Outcome: outcome}, nil
}

// mapProviderEventToOutcome maps a definitive normalized provider status
// (CAPTURED or FAILED — never PENDING, which callers must handle before
// reaching here) to the corresponding RecoveryCaseStatus,
// RecoveryOutcomeStatus, and resolved RecoveryActionStatus. This is the
// one place the CAPTURED/FAILED -> SUCCESS/FAILED mapping is defined, used
// identically by WebhookProcessor and ReconciliationEngine.
func mapProviderEventToOutcome(status domain.ProviderEventStatus) (domain.RecoveryCaseStatus, domain.RecoveryOutcomeStatus, domain.RecoveryActionStatus, error) {
	switch status {
	case domain.ProviderEventStatusCaptured:
		return domain.RecoveryCaseStatusSuccess, domain.RecoveryOutcomeStatusSuccess, domain.RecoveryActionStatusSucceeded, nil
	case domain.ProviderEventStatusFailed:
		return domain.RecoveryCaseStatusFailed, domain.RecoveryOutcomeStatusFailed, domain.RecoveryActionStatusFailed, nil
	default:
		return "", "", "", fmt.Errorf("service: %q is not a definitive provider event status", status)
	}
}
