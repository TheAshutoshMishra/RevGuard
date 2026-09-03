package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
)

// RecoveryCaseRepository persists and retrieves RecoveryCase entities.
type RecoveryCaseRepository interface {
	Create(ctx context.Context, c *domain.RecoveryCase) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryCase, error)
}

// PostgresRecoveryCaseRepository is the PostgreSQL-backed
// RecoveryCaseRepository.
type PostgresRecoveryCaseRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRecoveryCaseRepository(pool *pgxpool.Pool) *PostgresRecoveryCaseRepository {
	return &PostgresRecoveryCaseRepository{pool: pool}
}

func (r *PostgresRecoveryCaseRepository) Create(ctx context.Context, c *domain.RecoveryCase) error {
	const q = `
		INSERT INTO recovery_cases (
			id, merchant_id, customer_id, payment_id, status,
			revenue_at_risk_minor_units, currency, created_at, updated_at, closed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.pool.Exec(ctx, q,
		c.ID, c.MerchantID, c.CustomerID, c.PaymentID, string(c.Status),
		c.RevenueAtRisk.MinorUnits, string(c.RevenueAtRisk.Currency),
		c.CreatedAt, c.UpdatedAt, c.ClosedAt)
	return err
}

func (r *PostgresRecoveryCaseRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryCase, error) {
	const q = `
		SELECT id, merchant_id, customer_id, payment_id, status,
			revenue_at_risk_minor_units, currency, created_at, updated_at, closed_at
		FROM recovery_cases
		WHERE id = $1`
	var (
		c        domain.RecoveryCase
		status   string
		currency string
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.MerchantID, &c.CustomerID, &c.PaymentID, &status,
		&c.RevenueAtRisk.MinorUnits, &currency, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Status = domain.RecoveryCaseStatus(status)
	c.RevenueAtRisk.Currency = domain.Currency(currency)
	return &c, nil
}
