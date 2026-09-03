package repository

import (
	"context"
	"errors"

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
			idempotency_key, requested_at, executed_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.Exec(ctx, q,
		a.ID, a.RecoveryCaseID, string(a.ActionType), string(a.Status), a.AttemptNumber,
		a.IdempotencyKey, a.RequestedAt, a.ExecutedAt, a.CreatedAt)
	return err
}

func (r *PostgresRecoveryActionRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryAction, error) {
	const q = `
		SELECT id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at
		FROM recovery_actions
		WHERE id = $1`
	var (
		a          domain.RecoveryAction
		actionType string
		status     string
	)
	err := r.db.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.RecoveryCaseID, &actionType, &status, &a.AttemptNumber,
		&a.IdempotencyKey, &a.RequestedAt, &a.ExecutedAt, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.ActionType = domain.RecoveryActionType(actionType)
	a.Status = domain.RecoveryActionStatus(status)
	return &a, nil
}

func (r *PostgresRecoveryActionRepository) ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.RecoveryAction, error) {
	const q = `
		SELECT id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at
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
		var (
			a          domain.RecoveryAction
			actionType string
			status     string
		)
		if err := rows.Scan(
			&a.ID, &a.RecoveryCaseID, &actionType, &status, &a.AttemptNumber,
			&a.IdempotencyKey, &a.RequestedAt, &a.ExecutedAt, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		a.ActionType = domain.RecoveryActionType(actionType)
		a.Status = domain.RecoveryActionStatus(status)
		out = append(out, &a)
	}
	return out, rows.Err()
}
