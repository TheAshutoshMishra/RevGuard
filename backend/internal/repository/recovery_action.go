package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
)

// RecoveryActionRepository persists and retrieves RecoveryAction entities.
type RecoveryActionRepository interface {
	Create(ctx context.Context, a *domain.RecoveryAction) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryAction, error)
}

// PostgresRecoveryActionRepository is the PostgreSQL-backed
// RecoveryActionRepository.
type PostgresRecoveryActionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRecoveryActionRepository(pool *pgxpool.Pool) *PostgresRecoveryActionRepository {
	return &PostgresRecoveryActionRepository{pool: pool}
}

func (r *PostgresRecoveryActionRepository) Create(ctx context.Context, a *domain.RecoveryAction) error {
	const q = `
		INSERT INTO recovery_actions (
			id, recovery_case_id, action_type, status, attempt_number,
			idempotency_key, requested_at, executed_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.pool.Exec(ctx, q,
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
	err := r.pool.QueryRow(ctx, q, id).Scan(
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
