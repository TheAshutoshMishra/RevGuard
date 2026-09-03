package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// PaymentAttemptRepository persists and retrieves PaymentAttempt entities.
type PaymentAttemptRepository interface {
	Create(ctx context.Context, a *domain.PaymentAttempt) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentAttempt, error)
	// ListByPaymentID returns every attempt for a payment, ordered by
	// attempt number. Used by RecoveryContextBuilder to give the AI
	// service attempt history.
	ListByPaymentID(ctx context.Context, paymentID uuid.UUID) ([]*domain.PaymentAttempt, error)
}

// PostgresPaymentAttemptRepository is the PostgreSQL-backed
// PaymentAttemptRepository.
type PostgresPaymentAttemptRepository struct {
	db DBTX
}

func NewPostgresPaymentAttemptRepository(db DBTX) *PostgresPaymentAttemptRepository {
	return &PostgresPaymentAttemptRepository{db: db}
}

func (r *PostgresPaymentAttemptRepository) Create(ctx context.Context, a *domain.PaymentAttempt) error {
	const q = `
		INSERT INTO payment_attempts (
			id, payment_id, attempt_number, status, failure_code, failure_reason,
			started_at, completed_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.Exec(ctx, q,
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
	err := r.db.QueryRow(ctx, q, id).Scan(
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

func (r *PostgresPaymentAttemptRepository) ListByPaymentID(ctx context.Context, paymentID uuid.UUID) ([]*domain.PaymentAttempt, error) {
	const q = `
		SELECT id, payment_id, attempt_number, status, failure_code, failure_reason,
			started_at, completed_at, created_at
		FROM payment_attempts
		WHERE payment_id = $1
		ORDER BY attempt_number ASC`
	rows, err := r.db.Query(ctx, q, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.PaymentAttempt
	for rows.Next() {
		var (
			a      domain.PaymentAttempt
			status string
		)
		if err := rows.Scan(
			&a.ID, &a.PaymentID, &a.AttemptNumber, &status, &a.FailureCode, &a.FailureReason,
			&a.StartedAt, &a.CompletedAt, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		a.Status = domain.PaymentAttemptStatus(status)
		out = append(out, &a)
	}
	return out, rows.Err()
}
