package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// executionStaleAfter bounds how long a RecoveryAction may sit in
// EXECUTING before a later Execute call is willing to treat it as
// abandoned (e.g. the process that started it crashed before Phase 3)
// rather than "still genuinely in flight." Concurrent callers arriving
// within milliseconds of each other (the common case) never cross this
// threshold, so a legitimately in-progress execution is never stomped by
// a racing caller — see docs/architecture/execution-engine.md.
const executionStaleAfter = 30 * time.Second

// executableActions is the complete, authoritative list of
// domain.RecommendedAction values ExecutionEngine can actually carry
// out, mapped to the domain.RecoveryActionType persisted on the
// resulting RecoveryAction. Milestone 6 shipped only retry_payment;
// Milestone 10 added send_payment_link via the same PaymentProvider
// abstraction (see PaymentProvider.SendPaymentLink). An AuthorizedAction
// not in this map is genuinely authorized by policy but has no
// execution implementation yet — ErrActionNotExecutable, never a
// fabricated result. Extending real execution coverage to a new action
// is exactly "add an entry here, add the matching case in Execute's
// provider dispatch, add the matching PaymentProvider method" — no
// other engine code changes.
var executableActions = map[domain.RecommendedAction]domain.RecoveryActionType{
	domain.RecommendedActionRetryPayment:    domain.RecoveryActionTypeRetryPayment,
	domain.RecommendedActionSendPaymentLink: domain.RecoveryActionTypeSendPaymentLink,
}

// ExecutionOutcome describes what Execute did.
type ExecutionOutcome struct {
	Action *domain.RecoveryAction
	// Case reflects the RecoveryCase's status after this call.
	Case *domain.RecoveryCase
	// Created is true only if this call performed a fresh execution
	// attempt (created the RecoveryAction and called the provider).
	// False means an idempotent no-op: a terminal action already
	// existed, or an in-flight action was found too recently created to
	// treat as abandoned.
	Created bool
}

// ExecutionEngine turns an ALLOW PolicyDecision into a bounded, auditable
// execution attempt against a PaymentProvider.
//
// It never trusts a caller-supplied action: the only action ExecutionEngine
// ever executes is PolicyDecision.AuthorizedAction, loaded fresh from
// PostgreSQL for the exact policyDecisionID given — never a value passed
// in by an HTTP client. See Execute's doc comment for the full validation
// chain.
//
// The provider call happens outside any database transaction (the same
// two-phase principle AnalysisOrchestrator established for the AI service
// call in Milestone 3): Phase 1 validates and durably records that
// execution is starting; Phase 2 calls the provider with no transaction
// open; Phase 3 durably records the result. This protects database
// connections and locks from a slow or hanging external call.
type ExecutionEngine struct {
	pool     *pgxpool.Pool
	provider PaymentProvider
	logger   *slog.Logger
}

func NewExecutionEngine(pool *pgxpool.Pool, provider PaymentProvider, logger *slog.Logger) *ExecutionEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExecutionEngine{pool: pool, provider: provider, logger: logger}
}

// Execute validates policyDecisionID (which must belong to
// recoveryCaseID, must be ALLOW, and must carry a supported
// AuthorizedAction), then executes that action exactly once — even under
// retries or concurrent callers — via the configured PaymentProvider.
//
// Validation chain (any failure returns a typed error and performs no
// execution side effect):
//  1. PolicyDecision exists (ErrPolicyDecisionNotFound).
//  2. It belongs to recoveryCaseID (ErrPolicyDecisionCaseMismatch).
//  3. Its Outcome is ALLOW (ErrPolicyDecisionNotAllow) — BLOCK/ESCALATE
//     can never reach execution.
//  4. It has a non-empty, valid AuthorizedAction (ErrMissingAuthorizedAction).
//  5. That action has a real execution implementation
//     (ErrActionNotExecutable) — retry_payment (Milestone 6) and
//     send_payment_link (Milestone 10); see executableActions.
//  6. The RecoveryCase currently is ALLOW (ErrRecoveryCaseNotAllow) —
//     ANALYZED/DETECTED/BLOCK/ESCALATE/SUCCESS/FAILED/CLOSED all reject.
//
// Idempotency: the RecoveryAction's IdempotencyKey is deterministic
// ("policy-decision:<policyDecisionID>"), so at most one RecoveryAction
// row is ever created per PolicyDecision, and therefore per logical
// execution. A repeated or concurrent call for the same policyDecisionID
// never causes a second PaymentProvider call — see phase1's idempotency
// handling and docs/architecture/execution-engine.md.
func (e *ExecutionEngine) Execute(ctx context.Context, recoveryCaseID, policyDecisionID uuid.UUID) (*ExecutionOutcome, error) {
	p1, err := e.phase1(ctx, recoveryCaseID, policyDecisionID)
	if err != nil {
		return nil, err
	}
	if !p1.shouldCallProvider {
		return &ExecutionOutcome{Action: p1.action, Case: p1.recoveryCase, Created: false}, nil
	}

	// PHASE 2: outside any transaction. Dispatches to the PaymentProvider
	// method matching p1.action.ActionType (set from executableActions in
	// phase1); both methods report through the same
	// success/reference/error-code shape, converted to RetryPaymentResult
	// here purely so phase3's persistence/state-transition logic (which
	// is action-type-agnostic — it only cares about Succeeded/reference/
	// error-code) never needs to know which provider method was called.
	started := time.Now()
	e.logger.Info("execution provider call started",
		"recovery_case_id", recoveryCaseID, "recovery_action_id", p1.action.ID,
		"authorized_action", string(p1.action.ActionType), "provider", e.provider.Name())

	var result RetryPaymentResult
	var providerErr error
	switch p1.action.ActionType {
	case domain.RecoveryActionTypeSendPaymentLink:
		var linkResult SendPaymentLinkResult
		linkResult, providerErr = e.provider.SendPaymentLink(ctx, SendPaymentLinkRequest{
			IdempotencyKey:    p1.action.IdempotencyKey,
			ExternalPaymentID: p1.externalPaymentID,
			AmountMinorUnits:  p1.amountMinorUnits,
			Currency:          p1.currency,
		})
		result = RetryPaymentResult{
			Succeeded: linkResult.Succeeded, ProviderReference: linkResult.ProviderReference,
			ErrorCode: linkResult.ErrorCode, ErrorMessage: linkResult.ErrorMessage,
		}
	default:
		result, providerErr = e.provider.RetryPayment(ctx, RetryPaymentRequest{
			IdempotencyKey:    p1.action.IdempotencyKey,
			ExternalPaymentID: p1.externalPaymentID,
			AmountMinorUnits:  p1.amountMinorUnits,
			Currency:          p1.currency,
		})
	}
	latency := time.Since(started)
	e.logger.Info("execution provider call finished",
		"recovery_case_id", recoveryCaseID, "recovery_action_id", p1.action.ID,
		"provider", e.provider.Name(), "latency_ms", latency.Milliseconds(),
		"ambiguous", providerErr != nil)

	return e.phase3(ctx, recoveryCaseID, p1.action, p1.recoveryCase, result, providerErr)
}

type phase1Result struct {
	action             *domain.RecoveryAction
	recoveryCase       *domain.RecoveryCase
	shouldCallProvider bool
	externalPaymentID  string
	amountMinorUnits   int64
	currency           string
}

func (e *ExecutionEngine) phase1(ctx context.Context, recoveryCaseID, policyDecisionID uuid.UUID) (*phase1Result, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	decisionRepo := repository.NewPostgresPolicyDecisionRepository(tx)
	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	paymentRepo := repository.NewPostgresPaymentRepository(tx)
	actionRepo := repository.NewPostgresRecoveryActionRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)

	decision, err := decisionRepo.GetByID(ctx, policyDecisionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrPolicyDecisionNotFound, policyDecisionID)
		}
		return nil, fmt.Errorf("service: load policy decision: %w", err)
	}
	if decision.RecoveryCaseID != recoveryCaseID {
		return nil, fmt.Errorf("%w: decision %s belongs to case %s, not %s",
			ErrPolicyDecisionCaseMismatch, decision.ID, decision.RecoveryCaseID, recoveryCaseID)
	}
	if decision.Outcome != domain.PolicyDecisionOutcomeAllow {
		return nil, fmt.Errorf("%w: decision %s is %s", ErrPolicyDecisionNotAllow, decision.ID, decision.Outcome)
	}
	if decision.AuthorizedAction == "" || !decision.AuthorizedAction.Valid() {
		return nil, fmt.Errorf("%w: decision %s", ErrMissingAuthorizedAction, decision.ID)
	}
	// Only actions in executableActions have a real execution
	// implementation. The policy decision is still respected: nothing is
	// executed, no fabricated result is produced, and this is reported
	// as a clear, typed error.
	actionType, executable := executableActions[decision.AuthorizedAction]
	if !executable {
		return nil, fmt.Errorf("%w: %s", ErrActionNotExecutable, decision.AuthorizedAction)
	}

	idempotencyKey := "policy-decision:" + policyDecisionID.String()

	if existing, err := actionRepo.GetByIdempotencyKey(ctx, idempotencyKey); err == nil {
		return e.resumeExisting(ctx, tx, recoveryCaseID, existing, caseRepo, actionRepo, auditRepo)
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: check existing recovery action: %w", err)
	}

	recoveryCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrRecoveryCaseNotFound, recoveryCaseID)
		}
		return nil, fmt.Errorf("service: load recovery case: %w", err)
	}
	if recoveryCase.Status != domain.RecoveryCaseStatusAllow {
		// Mirror PolicyEngine's race-safety fix: under READ COMMITTED a
		// concurrent Execute call for this exact policyDecisionID may
		// have committed between our idempotency check above and this
		// status check. Re-check once more before treating it as a
		// genuine wrong-state error.
		if existing, err := actionRepo.GetByIdempotencyKey(ctx, idempotencyKey); err == nil {
			return e.resumeExisting(ctx, tx, recoveryCaseID, existing, caseRepo, actionRepo, auditRepo)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: re-check existing recovery action: %w", err)
		}
		return nil, fmt.Errorf("%w: case %s is %q", ErrRecoveryCaseNotAllow, recoveryCaseID, recoveryCase.Status)
	}

	payment, err := paymentRepo.GetByID(ctx, recoveryCase.PaymentID)
	if err != nil {
		return nil, fmt.Errorf("service: load payment: %w", err)
	}

	now := time.Now().UTC()
	newAction := &domain.RecoveryAction{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		ActionType:     actionType,
		Status:         domain.RecoveryActionStatusExecuting,
		AttemptNumber:  1,
		IdempotencyKey: idempotencyKey,
		RequestedAt:    now,
		CreatedAt:      now,
		Provider:       e.provider.Name(),
	}
	created, err := actionRepo.TryCreate(ctx, newAction)
	if err != nil {
		return nil, fmt.Errorf("service: persist recovery action: %w", err)
	}
	if !created {
		winner, err := actionRepo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("service: reload recovery action after race: %w", err)
		}
		return e.resumeExisting(ctx, tx, recoveryCaseID, winner, caseRepo, actionRepo, auditRepo)
	}

	if err := ValidateTransition(domain.RecoveryCaseStatusAllow, domain.RecoveryCaseStatusExecuting); err != nil {
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}
	if err := caseRepo.UpdateStatus(ctx, recoveryCaseID, domain.RecoveryCaseStatusAllow, domain.RecoveryCaseStatusExecuting, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: case status changed concurrently: %w", err)
		}
		return nil, fmt.Errorf("service: transition recovery case to EXECUTING: %w", err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		EventType:      "recovery_execution.started",
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"policy_decision_id": policyDecisionID,
			"recovery_action_id": newAction.ID,
			"authorized_action":  string(decision.AuthorizedAction),
			"provider":           e.provider.Name(),
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit execution start: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	recoveryCase.Status = domain.RecoveryCaseStatusExecuting
	recoveryCase.UpdatedAt = now

	e.logger.Info("recovery case execution started",
		"recovery_case_id", recoveryCaseID, "recovery_action_id", newAction.ID,
		"policy_decision_id", policyDecisionID, "authorized_action", string(decision.AuthorizedAction))

	return &phase1Result{
		action: newAction, recoveryCase: recoveryCase, shouldCallProvider: true,
		externalPaymentID: payment.ExternalPaymentID,
		amountMinorUnits:  payment.Amount.MinorUnits,
		currency:          string(payment.Amount.Currency),
	}, nil
}

// resumeExisting handles every "an action for this idempotency key
// already exists" path: a genuine idempotent retry (terminal status), a
// legitimately-still-in-flight concurrent call (recent EXECUTING), or an
// abandoned/orphaned execution (stale EXECUTING) that gets safely
// resolved to UNKNOWN without ever calling the provider again. It always
// commits (or leaves nothing to commit) and never calls Phase 2.
func (e *ExecutionEngine) resumeExisting(
	ctx context.Context, tx pgx.Tx,
	recoveryCaseID uuid.UUID, existing *domain.RecoveryAction,
	caseRepo repository.RecoveryCaseRepository, actionRepo repository.RecoveryActionRepository, auditRepo repository.AuditEventRepository,
) (*phase1Result, error) {
	if existing.Status != domain.RecoveryActionStatusExecuting {
		// Terminal: SUCCEEDED, FAILED, or UNKNOWN. Fully idempotent no-op.
		recoveryCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
		if err != nil {
			return nil, fmt.Errorf("service: load recovery case: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		e.logger.Info("recovery action already resolved; no-op",
			"recovery_case_id", recoveryCaseID, "recovery_action_id", existing.ID, "status", string(existing.Status))
		return &phase1Result{action: existing, recoveryCase: recoveryCase, shouldCallProvider: false}, nil
	}

	if time.Since(existing.RequestedAt) < executionStaleAfter {
		// Plausibly still genuinely in flight (another call or process is
		// between Phase 1 and Phase 3 right now). Report current state,
		// touch nothing, never call the provider ourselves.
		recoveryCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
		if err != nil {
			return nil, fmt.Errorf("service: load recovery case: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		e.logger.Info("recovery action still in flight; no-op",
			"recovery_case_id", recoveryCaseID, "recovery_action_id", existing.ID)
		return &phase1Result{action: existing, recoveryCase: recoveryCase, shouldCallProvider: false}, nil
	}

	// Stale: been EXECUTING for longer than executionStaleAfter. We
	// cannot know whether the abandoned attempt ever reached the
	// provider, so we do NOT call it again — resolve to UNKNOWN and let
	// Milestone 7's reconciliation establish the truth later.
	now := time.Now().UTC()
	metadata := auditJSON(map[string]any{
		"reason":           "execution_stale_no_result_recorded",
		"requested_at":     existing.RequestedAt,
		"stale_after_secs": executionStaleAfter.Seconds(),
	})
	if err := actionRepo.UpdateExecutionResult(ctx, existing.ID, domain.RecoveryActionStatusExecuting, domain.RecoveryActionStatusUnknown, now, "", "EXECUTION_STALE_AMBIGUOUS", metadata); err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: resolve stale recovery action: %w", err)
		}
		// Someone else resolved it between our read and this write;
		// reload and treat as already-resolved.
	}
	reloaded, err := actionRepo.GetByID(ctx, existing.ID)
	if err != nil {
		return nil, fmt.Errorf("service: reload resolved recovery action: %w", err)
	}

	if err := caseRepo.UpdateStatus(ctx, recoveryCaseID, domain.RecoveryCaseStatusExecuting, domain.RecoveryCaseStatusVerifying, now); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: transition recovery case to VERIFYING: %w", err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		EventType:      "recovery_execution.unknown",
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"recovery_action_id": existing.ID,
			"reason":             "execution_stale_no_result_recorded",
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit stale resolution: %w", err)
	}

	recoveryCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
	if err != nil {
		return nil, fmt.Errorf("service: load recovery case: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	e.logger.Warn("resolved abandoned execution to UNKNOWN without calling provider",
		"recovery_case_id", recoveryCaseID, "recovery_action_id", existing.ID)

	return &phase1Result{action: reloaded, recoveryCase: recoveryCase, shouldCallProvider: false}, nil
}

func (e *ExecutionEngine) phase3(ctx context.Context, recoveryCaseID uuid.UUID, action *domain.RecoveryAction, recoveryCase *domain.RecoveryCase, result RetryPaymentResult, providerErr error) (*ExecutionOutcome, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	actionRepo := repository.NewPostgresRecoveryActionRepository(tx)
	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)

	now := time.Now().UTC()
	var (
		newStatus         domain.RecoveryActionStatus
		providerReference string
		errorCode         string
		auditEventType    string
		metadata          map[string]any
	)

	switch {
	case providerErr != nil:
		// Ambiguous: timeout, transport error, or any other condition
		// where we cannot be sure the provider processed the request.
		// NEVER fabricated into SUCCEEDED or FAILED.
		newStatus = domain.RecoveryActionStatusUnknown
		errorCode = "PROVIDER_RESPONSE_AMBIGUOUS"
		auditEventType = "recovery_execution.unknown"
		metadata = map[string]any{"reason": "provider_response_ambiguous", "detail": providerErr.Error()}
	case result.Succeeded:
		newStatus = domain.RecoveryActionStatusSucceeded
		providerReference = result.ProviderReference
		auditEventType = "recovery_execution.completed"
		metadata = map[string]any{"outcome": "succeeded"}
	default:
		newStatus = domain.RecoveryActionStatusFailed
		errorCode = result.ErrorCode
		auditEventType = "recovery_execution.completed"
		metadata = map[string]any{"outcome": "failed", "error_code": result.ErrorCode}
	}

	if err := actionRepo.UpdateExecutionResult(ctx, action.ID, domain.RecoveryActionStatusExecuting, newStatus, now, providerReference, errorCode, auditJSON(metadata)); err != nil {
		return nil, fmt.Errorf("service: persist execution result: %w", err)
	}

	if err := ValidateTransition(domain.RecoveryCaseStatusExecuting, domain.RecoveryCaseStatusVerifying); err != nil {
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}
	if err := caseRepo.UpdateStatus(ctx, recoveryCaseID, domain.RecoveryCaseStatusExecuting, domain.RecoveryCaseStatusVerifying, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: case status changed concurrently: %w", err)
		}
		return nil, fmt.Errorf("service: transition recovery case to VERIFYING: %w", err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		EventType:      auditEventType,
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"recovery_action_id": action.ID,
			"provider":           action.Provider,
			"authorized_action":  string(action.ActionType),
			"status":             string(newStatus),
			"provider_reference": providerReference,
			"error_code":         errorCode,
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit execution result: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	action.Status = newStatus
	action.ExecutedAt = &now
	action.ProviderReference = providerReference
	action.ErrorCode = errorCode

	recoveryCase.Status = domain.RecoveryCaseStatusVerifying
	recoveryCase.UpdatedAt = now

	e.logger.Info("recovery case execution result recorded",
		"recovery_case_id", recoveryCaseID, "recovery_action_id", action.ID,
		"status", string(newStatus), "provider", action.Provider)

	return &ExecutionOutcome{Action: action, Case: recoveryCase, Created: true}, nil
}
