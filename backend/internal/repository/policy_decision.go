package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// PolicyDecisionRepository persists and retrieves PolicyDecision entities.
type PolicyDecisionRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.PolicyDecision, error)
	// GetByCaseDiagnosisEvaluationVersion looks up the (at most one, per
	// the UNIQUE constraint in migration 000014) decision for an exact
	// (case, diagnosis, evaluation, policy version) tuple. Used for the
	// idempotency check before evaluating.
	GetByCaseDiagnosisEvaluationVersion(ctx context.Context, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID uuid.UUID, policyVersion string) (*domain.PolicyDecision, error)
	// GetLatestByRecoveryCaseID returns the most recently created
	// decision for a case. Used by the read endpoint.
	GetLatestByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.PolicyDecision, error)
	// TryCreate inserts d unless a decision for the same
	// (case, diagnosis, evaluation, policy version) tuple already exists
	// (ON CONFLICT DO NOTHING), reporting created=false in that case.
	// Like RecoveryEconomicEvaluationRepository.TryCreate, this never
	// errors on conflict and therefore never poisons the enclosing
	// transaction.
	TryCreate(ctx context.Context, d *domain.PolicyDecision) (created bool, err error)
}

// PostgresPolicyDecisionRepository is the PostgreSQL-backed
// PolicyDecisionRepository.
type PostgresPolicyDecisionRepository struct {
	db DBTX
}

func NewPostgresPolicyDecisionRepository(db DBTX) *PostgresPolicyDecisionRepository {
	return &PostgresPolicyDecisionRepository{db: db}
}

func (r *PostgresPolicyDecisionRepository) TryCreate(ctx context.Context, d *domain.PolicyDecision) (bool, error) {
	reasonCodes, err := json.Marshal(d.ReasonCodes)
	if err != nil {
		return false, err
	}
	var authorizedAction *string
	if d.AuthorizedAction != "" {
		s := string(d.AuthorizedAction)
		authorizedAction = &s
	}
	const q = `
		INSERT INTO policy_decisions (
			id, recovery_case_id, recovery_diagnosis_id, recovery_economic_evaluation_id,
			decision, authorized_action, policy_version, reason_codes, explanation,
			evaluated_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (recovery_case_id, recovery_diagnosis_id, recovery_economic_evaluation_id, policy_version)
		DO NOTHING`
	tag, err := r.db.Exec(ctx, q,
		d.ID, d.RecoveryCaseID, d.RecoveryDiagnosisID, d.RecoveryEconomicEvaluationID,
		string(d.Outcome), authorizedAction, d.PolicyVersion, reasonCodes, d.Explanation,
		d.EvaluatedAt, d.CreatedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresPolicyDecisionRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.PolicyDecision, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_diagnosis_id, recovery_economic_evaluation_id,
			decision, authorized_action, policy_version, reason_codes, explanation,
			evaluated_at, created_at
		FROM policy_decisions
		WHERE id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, id))
}

func (r *PostgresPolicyDecisionRepository) GetByCaseDiagnosisEvaluationVersion(ctx context.Context, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID uuid.UUID, policyVersion string) (*domain.PolicyDecision, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_diagnosis_id, recovery_economic_evaluation_id,
			decision, authorized_action, policy_version, reason_codes, explanation,
			evaluated_at, created_at
		FROM policy_decisions
		WHERE recovery_case_id = $1 AND recovery_diagnosis_id = $2
			AND recovery_economic_evaluation_id = $3 AND policy_version = $4`
	return r.scanOne(r.db.QueryRow(ctx, q, recoveryCaseID, recoveryDiagnosisID, recoveryEconomicEvaluationID, policyVersion))
}

func (r *PostgresPolicyDecisionRepository) GetLatestByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.PolicyDecision, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_diagnosis_id, recovery_economic_evaluation_id,
			decision, authorized_action, policy_version, reason_codes, explanation,
			evaluated_at, created_at
		FROM policy_decisions
		WHERE recovery_case_id = $1
		ORDER BY created_at DESC
		LIMIT 1`
	return r.scanOne(r.db.QueryRow(ctx, q, recoveryCaseID))
}

func (r *PostgresPolicyDecisionRepository) scanOne(row pgx.Row) (*domain.PolicyDecision, error) {
	var (
		d                domain.PolicyDecision
		decision         string
		authorizedAction *string
		reasonCodes      []byte
	)
	err := row.Scan(
		&d.ID, &d.RecoveryCaseID, &d.RecoveryDiagnosisID, &d.RecoveryEconomicEvaluationID,
		&decision, &authorizedAction, &d.PolicyVersion, &reasonCodes, &d.Explanation,
		&d.EvaluatedAt, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Outcome = domain.PolicyDecisionOutcome(decision)
	if authorizedAction != nil {
		d.AuthorizedAction = domain.RecommendedAction(*authorizedAction)
	}
	if len(reasonCodes) > 0 {
		var codes []string
		if err := json.Unmarshal(reasonCodes, &codes); err != nil {
			return nil, err
		}
		d.ReasonCodes = make([]domain.PolicyReasonCode, len(codes))
		for i, c := range codes {
			d.ReasonCodes[i] = domain.PolicyReasonCode(c)
		}
	}
	return &d, nil
}
