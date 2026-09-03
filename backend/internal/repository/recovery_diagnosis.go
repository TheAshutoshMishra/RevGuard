package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// RecoveryDiagnosisRepository persists and retrieves RecoveryDiagnosis
// entities.
type RecoveryDiagnosisRepository interface {
	Create(ctx context.Context, d *domain.RecoveryDiagnosis) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryDiagnosis, error)
	// ListByRecoveryCaseID returns every diagnosis recorded for a case,
	// most recent first. A case can accumulate more than one (e.g.
	// re-analysis after a prior AI failure).
	ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.RecoveryDiagnosis, error)
}

// PostgresRecoveryDiagnosisRepository is the PostgreSQL-backed
// RecoveryDiagnosisRepository.
type PostgresRecoveryDiagnosisRepository struct {
	db DBTX
}

func NewPostgresRecoveryDiagnosisRepository(db DBTX) *PostgresRecoveryDiagnosisRepository {
	return &PostgresRecoveryDiagnosisRepository{db: db}
}

func (r *PostgresRecoveryDiagnosisRepository) Create(ctx context.Context, d *domain.RecoveryDiagnosis) error {
	riskFlags, err := json.Marshal(d.RiskFlags)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO recovery_diagnoses (
			id, recovery_case_id, failure_category, diagnosis_reason, customer_context,
			recommended_strategy, recommended_action, recommendation_reason, confidence,
			risk_flags, explanation, provider, model, prompt_version, generated_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`
	_, err = r.db.Exec(ctx, q,
		d.ID, d.RecoveryCaseID, string(d.FailureCategory), d.DiagnosisReason, d.CustomerContext,
		d.RecommendedStrategy, string(d.RecommendedAction), d.RecommendationReason, d.Confidence,
		riskFlags, d.Explanation, d.Provider, d.Model, d.PromptVersion, d.GeneratedAt, d.CreatedAt)
	return err
}

func (r *PostgresRecoveryDiagnosisRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryDiagnosis, error) {
	const q = `
		SELECT id, recovery_case_id, failure_category, diagnosis_reason, customer_context,
			recommended_strategy, recommended_action, recommendation_reason, confidence,
			risk_flags, explanation, provider, model, prompt_version, generated_at, created_at
		FROM recovery_diagnoses
		WHERE id = $1`
	d, err := r.scanOne(r.db.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (r *PostgresRecoveryDiagnosisRepository) ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.RecoveryDiagnosis, error) {
	const q = `
		SELECT id, recovery_case_id, failure_category, diagnosis_reason, customer_context,
			recommended_strategy, recommended_action, recommendation_reason, confidence,
			risk_flags, explanation, provider, model, prompt_version, generated_at, created_at
		FROM recovery_diagnoses
		WHERE recovery_case_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.Query(ctx, q, recoveryCaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.RecoveryDiagnosis
	for rows.Next() {
		d, err := r.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (Query).
type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PostgresRecoveryDiagnosisRepository) scanOne(row rowScanner) (*domain.RecoveryDiagnosis, error) {
	var (
		d               domain.RecoveryDiagnosis
		failureCategory string
		recommendedAct  string
		riskFlags       []byte
	)
	err := row.Scan(
		&d.ID, &d.RecoveryCaseID, &failureCategory, &d.DiagnosisReason, &d.CustomerContext,
		&d.RecommendedStrategy, &recommendedAct, &d.RecommendationReason, &d.Confidence,
		&riskFlags, &d.Explanation, &d.Provider, &d.Model, &d.PromptVersion, &d.GeneratedAt, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	d.FailureCategory = domain.FailureCategory(failureCategory)
	d.RecommendedAction = domain.RecommendedAction(recommendedAct)
	if len(riskFlags) > 0 {
		if err := json.Unmarshal(riskFlags, &d.RiskFlags); err != nil {
			return nil, err
		}
	}
	return &d, nil
}
