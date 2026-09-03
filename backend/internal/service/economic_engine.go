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

// EconomicEvaluationOutcome describes what Evaluate did.
type EconomicEvaluationOutcome struct {
	Evaluation *domain.RecoveryEconomicEvaluation
	// Created is true only if this call inserted a new evaluation row.
	// False means an evaluation for this diagnosis already existed and
	// was returned unchanged (the idempotency guard).
	Created bool
}

// EconomicEngine performs the deterministic economic evaluation of a
// RecoveryDiagnosis's recommendation: given the revenue at risk, an
// estimated recovery probability (via RecoveryProbabilityEstimator, never
// the AI's confidence), and the recommended action's cost/risk
// assumptions (via ActionEconomics), it computes expected gross recovery,
// action cost, risk cost, and expected incremental value, and persists
// the result.
//
// The Economic Engine makes no policy decision and never mutates
// RecoveryCase.Status — the case remains ANALYZED before and after
// evaluation. It requires no external network call (unlike
// AnalysisOrchestrator's AI call), so — unlike AnalyzeCase's two-phase
// structure — Evaluate does all of its work, including reads, inside a
// single short transaction.
type EconomicEngine struct {
	pool      *pgxpool.Pool
	estimator RecoveryProbabilityEstimator
	logger    *slog.Logger
}

func NewEconomicEngine(pool *pgxpool.Pool, estimator RecoveryProbabilityEstimator, logger *slog.Logger) *EconomicEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &EconomicEngine{pool: pool, estimator: estimator, logger: logger}
}

// Evaluate economically evaluates recoveryDiagnosisID (which must belong
// to recoveryCaseID) and persists a RecoveryEconomicEvaluation. It is
// idempotent: calling it again for a diagnosis that already has an
// evaluation returns that evaluation unchanged (Created=false) rather
// than computing or persisting anything new. A different
// recoveryDiagnosisID (e.g. from a subsequent re-analysis of the same
// case) gets its own, independent evaluation.
func (e *EconomicEngine) Evaluate(ctx context.Context, recoveryCaseID, recoveryDiagnosisID uuid.UUID) (*EconomicEvaluationOutcome, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	diagnosisRepo := repository.NewPostgresRecoveryDiagnosisRepository(tx)
	evaluationRepo := repository.NewPostgresRecoveryEconomicEvaluationRepository(tx)
	attemptRepo := repository.NewPostgresPaymentAttemptRepository(tx)
	actionRepo := repository.NewPostgresRecoveryActionRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)

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
	if !diagnosis.RecommendedAction.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRecommendedAction, diagnosis.RecommendedAction)
	}
	if !diagnosis.FailureCategory.Valid() {
		return nil, fmt.Errorf("%w: failure_category %q", domain.ErrInvalidProbability, diagnosis.FailureCategory)
	}

	// Idempotency check, before doing any computation: PostgreSQL's
	// UNIQUE(recovery_diagnosis_id) constraint is the durable authority,
	// but checking first avoids redundant work on the common "already
	// evaluated" path.
	if existing, err := evaluationRepo.GetByRecoveryDiagnosisID(ctx, recoveryDiagnosisID); err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("service: commit: %w", commitErr)
		}
		e.logger.Info("economic evaluation already exists; no-op",
			"recovery_case_id", recoveryCaseID, "recovery_diagnosis_id", recoveryDiagnosisID,
			"evaluation_id", existing.ID)
		return &EconomicEvaluationOutcome{Evaluation: existing, Created: false}, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: check existing economic evaluation: %w", err)
	}

	paymentAttempts, err := attemptRepo.ListByPaymentID(ctx, recoveryCase.PaymentID)
	if err != nil {
		return nil, fmt.Errorf("service: load payment attempts: %w", err)
	}
	priorActions, err := actionRepo.ListByRecoveryCaseID(ctx, recoveryCaseID)
	if err != nil {
		return nil, fmt.Errorf("service: load prior recovery actions: %w", err)
	}

	probEstimate, err := e.estimator.Estimate(ctx, recoveryCase, diagnosis, paymentAttempts, priorActions)
	if err != nil {
		return nil, fmt.Errorf("service: estimate recovery probability: %w", err)
	}

	actionEconomics, err := GetActionEconomics(diagnosis.RecommendedAction)
	if err != nil {
		return nil, err
	}

	revenueAtRisk := recoveryCase.RevenueAtRisk.MinorUnits
	expectedGrossRecoveryMinorUnits := calculateExpectedGrossRecovery(revenueAtRisk, probEstimate.ProbabilityBps)
	actionCostMinorUnits := actionEconomics.ActionCostMinorUnits
	riskCostMinorUnits := calculateRiskCost(revenueAtRisk, actionEconomics.RiskCostBps)
	incrementalValueMinorUnits := calculateExpectedIncrementalValue(expectedGrossRecoveryMinorUnits, actionCostMinorUnits, riskCostMinorUnits)

	currency := recoveryCase.RevenueAtRisk.Currency
	now := time.Now().UTC()

	expectedGrossRecovery, err := domain.NewMoney(expectedGrossRecoveryMinorUnits, currency)
	if err != nil {
		return nil, fmt.Errorf("service: build expected gross recovery money: %w", err)
	}
	actionCost, err := domain.NewMoney(actionCostMinorUnits, currency)
	if err != nil {
		return nil, fmt.Errorf("service: build action cost money: %w", err)
	}
	riskCost, err := domain.NewMoney(riskCostMinorUnits, currency)
	if err != nil {
		return nil, fmt.Errorf("service: build risk cost money: %w", err)
	}

	evaluation := &domain.RecoveryEconomicEvaluation{
		ID:                                 uuid.New(),
		RecoveryCaseID:                     recoveryCaseID,
		RecoveryDiagnosisID:                recoveryDiagnosisID,
		RecommendedAction:                  diagnosis.RecommendedAction,
		RevenueAtRisk:                      recoveryCase.RevenueAtRisk,
		RecoveryProbabilityBps:             probEstimate.ProbabilityBps,
		ExpectedGrossRecovery:              expectedGrossRecovery,
		ActionCost:                         actionCost,
		RiskCost:                           riskCost,
		ExpectedIncrementalValueMinorUnits: incrementalValueMinorUnits,
		EstimatorName:                      probEstimate.EstimatorName,
		EstimatorVersion:                   probEstimate.EstimatorVersion,
		EconomicModelVersion:               EconomicModelVersion,
		CreatedAt:                          now,
	}

	created, err := evaluationRepo.TryCreate(ctx, evaluation)
	if err != nil {
		return nil, fmt.Errorf("service: persist economic evaluation: %w", err)
	}
	if !created {
		// Lost a race with a concurrent evaluation of the same diagnosis.
		// ON CONFLICT DO NOTHING never errors, so the transaction is
		// still healthy — just re-read the winner's row.
		existing, err := evaluationRepo.GetByRecoveryDiagnosisID(ctx, recoveryDiagnosisID)
		if err != nil {
			return nil, fmt.Errorf("service: reload economic evaluation after race: %w", err)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("service: commit: %w", commitErr)
		}
		e.logger.Info("lost race to create economic evaluation; returning winner",
			"recovery_case_id", recoveryCaseID, "recovery_diagnosis_id", recoveryDiagnosisID,
			"evaluation_id", existing.ID)
		return &EconomicEvaluationOutcome{Evaluation: existing, Created: false}, nil
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		EventType:      "recovery_economics.evaluated",
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"recovery_diagnosis_id":                  recoveryDiagnosisID,
			"recommended_action":                     string(diagnosis.RecommendedAction),
			"recovery_probability_bps":               int(probEstimate.ProbabilityBps),
			"expected_gross_recovery_minor_units":    expectedGrossRecoveryMinorUnits,
			"action_cost_minor_units":                actionCostMinorUnits,
			"risk_cost_minor_units":                  riskCostMinorUnits,
			"expected_incremental_value_minor_units": incrementalValueMinorUnits,
			"currency":                               string(currency),
			"economic_model_version":                 EconomicModelVersion,
			"estimator_name":                         probEstimate.EstimatorName,
			"estimator_version":                      probEstimate.EstimatorVersion,
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit economic evaluation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	e.logger.Info("recovery case economically evaluated",
		"recovery_case_id", recoveryCaseID, "recovery_diagnosis_id", recoveryDiagnosisID,
		"recommended_action", string(diagnosis.RecommendedAction),
		"recovery_probability_bps", int(probEstimate.ProbabilityBps),
		"expected_gross_recovery_minor_units", expectedGrossRecoveryMinorUnits,
		"action_cost_minor_units", actionCostMinorUnits,
		"risk_cost_minor_units", riskCostMinorUnits,
		"expected_incremental_value_minor_units", incrementalValueMinorUnits)

	return &EconomicEvaluationOutcome{Evaluation: evaluation, Created: true}, nil
}

// GetLatestEvaluation returns the most recently created economic
// evaluation for a case, or repository.ErrNotFound if none exists yet.
// Read-only; used by the GET /v1/recovery-cases/{id}/economic-evaluation
// endpoint.
func (e *EconomicEngine) GetLatestEvaluation(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.RecoveryEconomicEvaluation, error) {
	return repository.NewPostgresRecoveryEconomicEvaluationRepository(e.pool).GetLatestByRecoveryCaseID(ctx, recoveryCaseID)
}
