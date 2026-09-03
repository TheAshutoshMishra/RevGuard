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

// PolicyEvaluationOutcome describes what Evaluate did.
type PolicyEvaluationOutcome struct {
	Decision *domain.PolicyDecision
	// Case reflects the RecoveryCase's status after this call: the new
	// ALLOW/BLOCK/ESCALATE status on a fresh evaluation, or its
	// (unchanged) current status on an idempotent no-op.
	Case *domain.RecoveryCase
	// Created is true only if this call inserted a new decision row and
	// performed the state transition. False means a decision for this
	// exact (case, diagnosis, evaluation, policy version) tuple already
	// existed and was returned unchanged.
	Created bool
}

// PolicyEngine deterministically decides whether a diagnosed,
// economically-evaluated recommendation is authorized to proceed:
// ALLOW, BLOCK, or ESCALATE. It is the sole authority for this decision —
// see evaluatePolicyRules in policy_rules.go for the actual rule logic,
// kept in a separate pure-function file so it needs no database to test.
//
// The Policy Engine makes no external network call (no AI service, no
// payment gateway, nothing), so — like EconomicEngine and unlike
// AnalysisOrchestrator's AI call — Evaluate does all of its work,
// including reads, inside a single short transaction.
//
// PolicyEngine never executes anything: an ALLOW decision means the
// recommendation is authorized to proceed to a future execution
// milestone, not that anything has happened. No RecoveryAction is
// created here.
type PolicyEngine struct {
	pool   *pgxpool.Pool
	config PolicyConfig
	logger *slog.Logger
}

func NewPolicyEngine(pool *pgxpool.Pool, config PolicyConfig, logger *slog.Logger) *PolicyEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &PolicyEngine{pool: pool, config: config, logger: logger}
}

// Evaluate applies deterministic policy rules to recoveryDiagnosisID and
// recoveryEconomicEvaluationID (both of which must belong to
// recoveryCaseID) and persists an immutable PolicyDecision, transitioning
// the case from ANALYZED to POLICY_CHECK and then to the decision's
// outcome (ALLOW/BLOCK/ESCALATE) in the same transaction.
//
// It is idempotent: calling it again for the exact same
// (case, diagnosis, evaluation, policy version) tuple returns the
// existing decision unchanged (Created=false) rather than computing or
// persisting anything new, and never re-transitions the case. A new
// diagnosis or a new economic evaluation (e.g. from re-analysis) — or a
// new policy version — produces its own, independent decision.
func (e *PolicyEngine) Evaluate(ctx context.Context, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID uuid.UUID) (*PolicyEvaluationOutcome, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	diagnosisRepo := repository.NewPostgresRecoveryDiagnosisRepository(tx)
	evaluationRepo := repository.NewPostgresRecoveryEconomicEvaluationRepository(tx)
	decisionRepo := repository.NewPostgresPolicyDecisionRepository(tx)
	attemptRepo := repository.NewPostgresPaymentAttemptRepository(tx)
	actionRepo := repository.NewPostgresRecoveryActionRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)

	// Idempotency check first, before loading/validating anything else:
	// if this exact tuple was already decided, that prior call already
	// did all the validation and the transition. Re-validating now would
	// be redundant, and — importantly — the case's *current* status is a
	// legitimate consequence of that prior decision (e.g. now BLOCK), not
	// an error condition, so checking "is the case ANALYZED?" before this
	// idempotency check would incorrectly reject a safe retry.
	if existing, err := decisionRepo.GetByCaseDiagnosisEvaluationVersion(ctx, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID, e.config.Version); err == nil {
		recoveryCase, caseErr := caseRepo.GetByID(ctx, recoveryCaseID)
		if caseErr != nil {
			return nil, fmt.Errorf("service: load recovery case: %w", caseErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("service: commit: %w", commitErr)
		}
		e.logger.Info("policy decision already exists; no-op",
			"recovery_case_id", recoveryCaseID, "recovery_diagnosis_id", recoveryDiagnosisID,
			"recovery_economic_evaluation_id", recoveryEconomicEvaluationID, "decision_id", existing.ID)
		return &PolicyEvaluationOutcome{Decision: existing, Case: recoveryCase, Created: false}, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: check existing policy decision: %w", err)
	}

	recoveryCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrRecoveryCaseNotFound, recoveryCaseID)
		}
		return nil, fmt.Errorf("service: load recovery case: %w", err)
	}

	diagnosis, err := diagnosisRepo.GetByID(ctx, recoveryDiagnosisID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrRecoveryDiagnosisNotFound, recoveryDiagnosisID)
		}
		return nil, fmt.Errorf("service: load recovery diagnosis: %w", err)
	}
	if diagnosis.RecoveryCaseID != recoveryCaseID {
		return nil, fmt.Errorf("%w: diagnosis %s belongs to case %s, not %s",
			ErrDiagnosisCaseMismatch, diagnosis.ID, diagnosis.RecoveryCaseID, recoveryCaseID)
	}

	economicEvaluation, err := evaluationRepo.GetByID(ctx, recoveryEconomicEvaluationID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrRecoveryEconomicEvaluationNotFound, recoveryEconomicEvaluationID)
		}
		return nil, fmt.Errorf("service: load recovery economic evaluation: %w", err)
	}
	if economicEvaluation.RecoveryCaseID != recoveryCaseID {
		return nil, fmt.Errorf("%w: evaluation %s belongs to case %s, not %s",
			ErrEconomicEvaluationCaseMismatch, economicEvaluation.ID, economicEvaluation.RecoveryCaseID, recoveryCaseID)
	}
	if economicEvaluation.RecoveryDiagnosisID != recoveryDiagnosisID {
		return nil, fmt.Errorf("%w: evaluation %s was computed for diagnosis %s, not %s",
			ErrEconomicEvaluationDiagnosisMismatch, economicEvaluation.ID, economicEvaluation.RecoveryDiagnosisID, recoveryDiagnosisID)
	}

	if recoveryCase.Status != domain.RecoveryCaseStatusAnalyzed {
		// Under READ COMMITTED, each SELECT in this transaction sees a
		// fresh snapshot, not one consistent snapshot for the whole
		// transaction — so a concurrent evaluation of this exact tuple
		// can commit (decision + case transition) in the gap between our
		// idempotency check above and this status check. Before treating
		// that as a genuine wrong-state error, re-check idempotency once
		// more: if a concurrent call really did just finish, its
		// decision is now visible (it's already committed, which is
		// exactly why we're observing its case-status side effect), and
		// this is a safe retry, not an error.
		if existing, err := decisionRepo.GetByCaseDiagnosisEvaluationVersion(ctx, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID, e.config.Version); err == nil {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return nil, fmt.Errorf("service: commit: %w", commitErr)
			}
			e.logger.Info("policy decision completed concurrently; no-op",
				"recovery_case_id", recoveryCaseID, "recovery_diagnosis_id", recoveryDiagnosisID,
				"recovery_economic_evaluation_id", recoveryEconomicEvaluationID, "decision_id", existing.ID)
			return &PolicyEvaluationOutcome{Decision: existing, Case: recoveryCase, Created: false}, nil
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: re-check existing policy decision: %w", err)
		}
		return nil, fmt.Errorf("%w: case %s is %q", ErrRecoveryCaseNotAnalyzed, recoveryCaseID, recoveryCase.Status)
	}

	paymentAttempts, err := attemptRepo.ListByPaymentID(ctx, recoveryCase.PaymentID)
	if err != nil {
		return nil, fmt.Errorf("service: load payment attempts: %w", err)
	}
	priorActions, err := actionRepo.ListByRecoveryCaseID(ctx, recoveryCaseID)
	if err != nil {
		return nil, fmt.Errorf("service: load prior recovery actions: %w", err)
	}

	result := evaluatePolicyRules(e.config, PolicyRuleInput{
		Diagnosis:                diagnosis,
		EconomicEvaluation:       economicEvaluation,
		PaymentAttemptCount:      len(paymentAttempts),
		PriorRecoveryActionCount: len(priorActions),
	})

	now := time.Now().UTC()
	authorizedAction := domain.RecommendedAction("")
	if result.Outcome == domain.PolicyDecisionOutcomeAllow {
		authorizedAction = diagnosis.RecommendedAction
	}

	decision := &domain.PolicyDecision{
		ID:                           uuid.New(),
		RecoveryCaseID:               recoveryCaseID,
		RecoveryDiagnosisID:          recoveryDiagnosisID,
		RecoveryEconomicEvaluationID: recoveryEconomicEvaluationID,
		Outcome:                      result.Outcome,
		AuthorizedAction:             authorizedAction,
		PolicyVersion:                e.config.Version,
		ReasonCodes:                  result.ReasonCodes,
		Explanation:                  result.Explanation,
		EvaluatedAt:                  now,
		CreatedAt:                    now,
	}

	created, err := decisionRepo.TryCreate(ctx, decision)
	if err != nil {
		return nil, fmt.Errorf("service: persist policy decision: %w", err)
	}
	if !created {
		// Lost a race with a concurrent evaluation of the same tuple.
		// ON CONFLICT DO NOTHING never errors, so the transaction is
		// still healthy — just re-read the winner's row and its
		// resulting case status, without performing our own transition.
		existing, err := decisionRepo.GetByCaseDiagnosisEvaluationVersion(ctx, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID, e.config.Version)
		if err != nil {
			return nil, fmt.Errorf("service: reload policy decision after race: %w", err)
		}
		latestCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
		if err != nil {
			return nil, fmt.Errorf("service: reload recovery case after race: %w", err)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("service: commit: %w", commitErr)
		}
		e.logger.Info("lost race to create policy decision; returning winner",
			"recovery_case_id", recoveryCaseID, "decision_id", existing.ID)
		return &PolicyEvaluationOutcome{Decision: existing, Case: latestCase, Created: false}, nil
	}

	targetStatus := domain.RecoveryCaseStatus(result.Outcome)
	if err := ValidateTransition(domain.RecoveryCaseStatusAnalyzed, domain.RecoveryCaseStatusPolicyCheck); err != nil {
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}
	if err := ValidateTransition(domain.RecoveryCaseStatusPolicyCheck, targetStatus); err != nil {
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}

	// The state machine models POLICY_CHECK as a real intermediate
	// state, but nothing outside this function ever observes a case
	// sitting in POLICY_CHECK (no external call happens between the two
	// transitions) — so both hops happen back-to-back inside this one
	// transaction rather than being split into two separately-committed
	// steps.
	if err := caseRepo.UpdateStatus(ctx, recoveryCaseID, domain.RecoveryCaseStatusAnalyzed, domain.RecoveryCaseStatusPolicyCheck, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: case status changed concurrently during policy evaluation: %w", err)
		}
		return nil, fmt.Errorf("service: transition recovery case to POLICY_CHECK: %w", err)
	}
	if err := caseRepo.UpdateStatus(ctx, recoveryCaseID, domain.RecoveryCaseStatusPolicyCheck, targetStatus, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: case status changed concurrently during policy evaluation: %w", err)
		}
		return nil, fmt.Errorf("service: transition recovery case to %s: %w", targetStatus, err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		EventType:      "recovery_policy.evaluated",
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"recovery_diagnosis_id":                  recoveryDiagnosisID,
			"recovery_economic_evaluation_id":        recoveryEconomicEvaluationID,
			"decision":                               string(result.Outcome),
			"authorized_action":                      string(authorizedAction),
			"policy_version":                         e.config.Version,
			"reason_codes":                           decision.ReasonCodes,
			"revenue_at_risk_minor_units":            economicEvaluation.RevenueAtRisk.MinorUnits,
			"expected_incremental_value_minor_units": economicEvaluation.ExpectedIncrementalValueMinorUnits,
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit policy decision: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	recoveryCase.Status = targetStatus
	recoveryCase.UpdatedAt = now

	e.logger.Info("recovery case policy-evaluated",
		"recovery_case_id", recoveryCaseID, "decision", string(result.Outcome),
		"reason_codes", decision.ReasonCodes, "policy_version", e.config.Version)

	return &PolicyEvaluationOutcome{Decision: decision, Case: recoveryCase, Created: true}, nil
}

// GetLatestDecision returns the most recently created policy decision for
// a case, or repository.ErrNotFound if none exists yet. Read-only; used
// by the GET /v1/recovery-cases/{id}/policy-decision endpoint.
func (e *PolicyEngine) GetLatestDecision(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.PolicyDecision, error) {
	return repository.NewPostgresPolicyDecisionRepository(e.pool).GetLatestByRecoveryCaseID(ctx, recoveryCaseID)
}
