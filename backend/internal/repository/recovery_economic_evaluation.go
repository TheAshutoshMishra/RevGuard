package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// RecoveryEconomicEvaluationRepository persists and retrieves
// RecoveryEconomicEvaluation entities.
type RecoveryEconomicEvaluationRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryEconomicEvaluation, error)
	// GetByRecoveryDiagnosisID looks up the (at most one, per the
	// UNIQUE(recovery_diagnosis_id) constraint) evaluation for a
	// diagnosis. Used for the idempotency check before evaluating.
	GetByRecoveryDiagnosisID(ctx context.Context, recoveryDiagnosisID uuid.UUID) (*domain.RecoveryEconomicEvaluation, error)
	// GetLatestByRecoveryCaseID returns the most recently created
	// evaluation for a case (a case can accumulate more than one, e.g.
	// after re-analysis). Used by the read endpoint.
	GetLatestByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.RecoveryEconomicEvaluation, error)
	// TryCreate inserts e unless an evaluation for the same
	// RecoveryDiagnosisID already exists (ON CONFLICT DO NOTHING),
	// reporting created=false in that case. Unlike a plain INSERT hitting
	// a unique-violation error, ON CONFLICT DO NOTHING never errors and
	// never poisons the enclosing transaction — the caller can safely
	// query GetByRecoveryDiagnosisID afterward in the same transaction.
	TryCreate(ctx context.Context, e *domain.RecoveryEconomicEvaluation) (created bool, err error)
}

// PostgresRecoveryEconomicEvaluationRepository is the PostgreSQL-backed
// RecoveryEconomicEvaluationRepository.
type PostgresRecoveryEconomicEvaluationRepository struct {
	db DBTX
}

func NewPostgresRecoveryEconomicEvaluationRepository(db DBTX) *PostgresRecoveryEconomicEvaluationRepository {
	return &PostgresRecoveryEconomicEvaluationRepository{db: db}
}

func (r *PostgresRecoveryEconomicEvaluationRepository) TryCreate(ctx context.Context, e *domain.RecoveryEconomicEvaluation) (bool, error) {
	const q = `
		INSERT INTO recovery_economic_evaluations (
			id, recovery_case_id, recovery_diagnosis_id, recommended_action,
			revenue_at_risk_minor_units, recovery_probability_bps, expected_gross_recovery_minor_units,
			action_cost_minor_units, risk_cost_minor_units, expected_incremental_value_minor_units,
			currency, estimator_name, estimator_version, economic_model_version, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (recovery_diagnosis_id) DO NOTHING`
	tag, err := r.db.Exec(ctx, q,
		e.ID, e.RecoveryCaseID, e.RecoveryDiagnosisID, string(e.RecommendedAction),
		e.RevenueAtRisk.MinorUnits, int32(e.RecoveryProbabilityBps), e.ExpectedGrossRecovery.MinorUnits,
		e.ActionCost.MinorUnits, e.RiskCost.MinorUnits, e.ExpectedIncrementalValueMinorUnits,
		string(e.RevenueAtRisk.Currency), e.EstimatorName, e.EstimatorVersion, e.EconomicModelVersion, e.CreatedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresRecoveryEconomicEvaluationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryEconomicEvaluation, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_diagnosis_id, recommended_action,
			revenue_at_risk_minor_units, recovery_probability_bps, expected_gross_recovery_minor_units,
			action_cost_minor_units, risk_cost_minor_units, expected_incremental_value_minor_units,
			currency, estimator_name, estimator_version, economic_model_version, created_at
		FROM recovery_economic_evaluations
		WHERE id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, id))
}

func (r *PostgresRecoveryEconomicEvaluationRepository) GetByRecoveryDiagnosisID(ctx context.Context, recoveryDiagnosisID uuid.UUID) (*domain.RecoveryEconomicEvaluation, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_diagnosis_id, recommended_action,
			revenue_at_risk_minor_units, recovery_probability_bps, expected_gross_recovery_minor_units,
			action_cost_minor_units, risk_cost_minor_units, expected_incremental_value_minor_units,
			currency, estimator_name, estimator_version, economic_model_version, created_at
		FROM recovery_economic_evaluations
		WHERE recovery_diagnosis_id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, recoveryDiagnosisID))
}

func (r *PostgresRecoveryEconomicEvaluationRepository) GetLatestByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.RecoveryEconomicEvaluation, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_diagnosis_id, recommended_action,
			revenue_at_risk_minor_units, recovery_probability_bps, expected_gross_recovery_minor_units,
			action_cost_minor_units, risk_cost_minor_units, expected_incremental_value_minor_units,
			currency, estimator_name, estimator_version, economic_model_version, created_at
		FROM recovery_economic_evaluations
		WHERE recovery_case_id = $1
		ORDER BY created_at DESC
		LIMIT 1`
	return r.scanOne(r.db.QueryRow(ctx, q, recoveryCaseID))
}

func (r *PostgresRecoveryEconomicEvaluationRepository) scanOne(row pgx.Row) (*domain.RecoveryEconomicEvaluation, error) {
	var (
		e                 domain.RecoveryEconomicEvaluation
		recommendedAction string
		probabilityBps    int32
		currency          string
	)
	err := row.Scan(
		&e.ID, &e.RecoveryCaseID, &e.RecoveryDiagnosisID, &recommendedAction,
		&e.RevenueAtRisk.MinorUnits, &probabilityBps, &e.ExpectedGrossRecovery.MinorUnits,
		&e.ActionCost.MinorUnits, &e.RiskCost.MinorUnits, &e.ExpectedIncrementalValueMinorUnits,
		&currency, &e.EstimatorName, &e.EstimatorVersion, &e.EconomicModelVersion, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.RecommendedAction = domain.RecommendedAction(recommendedAction)
	e.RecoveryProbabilityBps = domain.ProbabilityBasisPoints(probabilityBps)
	e.RevenueAtRisk.Currency = domain.Currency(currency)
	e.ExpectedGrossRecovery.Currency = domain.Currency(currency)
	e.ActionCost.Currency = domain.Currency(currency)
	e.RiskCost.Currency = domain.Currency(currency)
	return &e, nil
}
