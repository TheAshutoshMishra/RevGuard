package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
)

// PaymentAttemptRepository persists and retrieves PaymentAttempt entities.
type PaymentAttemptRepository interface {
	Create(ctx context.Context, a *domain.PaymentAttempt) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentAttempt, error)
}

// PostgresPaymentAttemptRepository is the PostgreSQL-backed
// PaymentAttemptRepository.
type PostgresPaymentAttemptRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresPaymentAttemptRepository(pool *pgxpool.Pool) *PostgresPaymentAttemptRepository {
	return &PostgresPaymentAttemptRepository{pool: pool}
}

func (r *PostgresPaymentAttemptRepository) Create(ctx context.Context, a *domain.PaymentAttempt) error {
	const q = `
		INSERT INTO payment_attempts (
			id, payment_id, attempt_number, status, failure_code, failure_reason,
			started_at, completed_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.pool.Exec(ctx, q,
		a.ID, a.PaymentID, a.AttemptNumber, string(a.Status), a.FailureCode, a.FailureReason,
		a.StartedAt, a.CompletedAt, a.CreatedAt)
	return err
}

func (r *PostgresPaymentAttemptRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentAttempt, error) {
	const q = `
		SELECT id, payment_id, attempt_number, status, failure_code, failure_reason,
			started_at, completed_at, created_at
		FROM payment_attempts
		WHERE id = $1`
	var (
		a      domain.PaymentAttempt
		status string
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.PaymentID, &a.AttemptNumber, &status, &a.FailureCode, &a.FailureReason,
		&a.StartedAt, &a.CompletedAt, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Status = domain.PaymentAttemptStatus(status)
	return &a, nil
}
