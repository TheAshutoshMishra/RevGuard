package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// RecoveryActionRepository persists and retrieves RecoveryAction entities.
type RecoveryActionRepository interface {
	Create(ctx context.Context, a *domain.RecoveryAction) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryAction, error)
	// ListByRecoveryCaseID returns every action recorded for a case,
	// ordered by attempt number. Used by RecoveryContextBuilder to give
	// the AI service prior-recovery-attempt history.
	ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.RecoveryAction, error)
	// GetByIdempotencyKey looks up the (at most one, per the
	// UNIQUE(idempotency_key) constraint) action for a logical execution.
	// Used for the idempotency check before executing (Milestone 6).
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.RecoveryAction, error)
	// TryCreate inserts a unless an action with the same IdempotencyKey
	// already exists (ON CONFLICT DO NOTHING), reporting created=false in
	// that case. Never errors on conflict, so it never poisons the
	// enclosing transaction — same pattern as
	// RecoveryEconomicEvaluationRepository.TryCreate (Milestone 4).
	TryCreate(ctx context.Context, a *domain.RecoveryAction) (created bool, err error)
	// UpdateExecutionResult is a guarded state transition for an
	// execution attempt: it only updates the row if its current status
	// still matches `from`, returning ErrNotFound otherwise (someone
	// else already resolved it). Callers should validate the transition
	// with service.ValidateTransition semantics at the RecoveryCase
	// level separately — this guards the RecoveryAction row itself.
	UpdateExecutionResult(ctx context.Context, id uuid.UUID, from, to domain.RecoveryActionStatus, executedAt time.Time, providerReference, errorCode string, executionMetadata []byte) error
}

// PostgresRecoveryActionRepository is the PostgreSQL-backed
// RecoveryActionRepository.
type PostgresRecoveryActionRepository struct {
	db DBTX
}

func NewPostgresRecoveryActionRepository(db DBTX) *PostgresRecoveryActionRepository {
	return &PostgresRecoveryActionRepository{db: db}
}

func (r *PostgresRecoveryActionRepository) Create(ctx context.Context, a *domain.RecoveryAction) error {
	const q = `
		INSERT INTO recovery_actions (
			id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at,
			provider, provider_reference, error_code, execution_metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	_, err := r.db.Exec(ctx, q,
		a.ID, a.RecoveryCaseID, string(a.ActionType), string(a.Status), a.AttemptNumber,
		a.IdempotencyKey, a.RequestedAt, a.ExecutedAt, a.CreatedAt,
		nullableString(a.Provider), nullableString(a.ProviderReference), nullableString(a.ErrorCode),
		nonNilJSON(a.ExecutionMetadata))
	return err
}

func (r *PostgresRecoveryActionRepository) TryCreate(ctx context.Context, a *domain.RecoveryAction) (bool, error) {
	const q = `
		INSERT INTO recovery_actions (
			id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at,
			provider, provider_reference, error_code, execution_metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (idempotency_key) DO NOTHING`
	tag, err := r.db.Exec(ctx, q,
		a.ID, a.RecoveryCaseID, string(a.ActionType), string(a.Status), a.AttemptNumber,
		a.IdempotencyKey, a.RequestedAt, a.ExecutedAt, a.CreatedAt,
		nullableString(a.Provider), nullableString(a.ProviderReference), nullableString(a.ErrorCode),
		nonNilJSON(a.ExecutionMetadata))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresRecoveryActionRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryAction, error) {
	const q = `
		SELECT id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at,
			provider, provider_reference, error_code, execution_metadata
		FROM recovery_actions
		WHERE id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, id))
}

func (r *PostgresRecoveryActionRepository) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.RecoveryAction, error) {
	const q = `
		SELECT id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at,
			provider, provider_reference, error_code, execution_metadata
		FROM recovery_actions
		WHERE idempotency_key = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, idempotencyKey))
}

func (r *PostgresRecoveryActionRepository) ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.RecoveryAction, error) {
	const q = `
		SELECT id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at,
			provider, provider_reference, error_code, execution_metadata
		FROM recovery_actions
		WHERE recovery_case_id = $1
		ORDER BY attempt_number ASC`
	rows, err := r.db.Query(ctx, q, recoveryCaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.RecoveryAction
	for rows.Next() {
		a, err := r.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresRecoveryActionRepository) UpdateExecutionResult(
	ctx context.Context, id uuid.UUID, from, to domain.RecoveryActionStatus, executedAt time.Time,
	providerReference, errorCode string, executionMetadata []byte,
) error {
	const q = `
		UPDATE recovery_actions
		SET status = $1, executed_at = $2, provider_reference = $3, error_code = $4, execution_metadata = $5
		WHERE id = $6 AND status = $7`
	tag, err := r.db.Exec(ctx, q,
		string(to), executedAt, nullableString(providerReference), nullableString(errorCode),
		nonNilJSON(executionMetadata), id, string(from))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRecoveryActionRepository) scanOne(row rowScanner) (*domain.RecoveryAction, error) {
	var (
		a                 domain.RecoveryAction
		actionType        string
		status            string
		provider          *string
		providerReference *string
		errorCode         *string
		executionMetadata []byte
	)
	err := row.Scan(
		&a.ID, &a.RecoveryCaseID, &actionType, &status, &a.AttemptNumber,
		&a.IdempotencyKey, &a.RequestedAt, &a.ExecutedAt, &a.CreatedAt,
		&provider, &providerReference, &errorCode, &executionMetadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.ActionType = domain.RecoveryActionType(actionType)
	a.Status = domain.RecoveryActionStatus(status)
	if provider != nil {
		a.Provider = *provider
	}
	if providerReference != nil {
		a.ProviderReference = *providerReference
	}
	if errorCode != nil {
		a.ErrorCode = *errorCode
	}
	a.ExecutionMetadata = executionMetadata
	return &a, nil
}

// nullableString converts an empty Go string (this codebase's established
// "unset" convention for optional string fields — see e.g.
// domain.PolicyDecision.AuthorizedAction) to a SQL NULL.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nonNilJSON ensures a JSONB column never receives a nil/empty []byte,
// which pgx would otherwise send as SQL NULL rather than a valid JSON
// value.
func nonNilJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte(`{}`)
	}
	return b
}
