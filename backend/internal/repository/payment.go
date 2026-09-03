package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// PaymentRepository persists and retrieves Payment entities.
type PaymentRepository interface {
	Create(ctx context.Context, p *domain.Payment) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
}

// PostgresPaymentRepository is the PostgreSQL-backed PaymentRepository.
type PostgresPaymentRepository struct {
	db DBTX
}

func NewPostgresPaymentRepository(db DBTX) *PostgresPaymentRepository {
	return &PostgresPaymentRepository{db: db}
}

func (r *PostgresPaymentRepository) Create(ctx context.Context, p *domain.Payment) error {
	const q = `
		INSERT INTO payments (
			id, merchant_id, customer_id, external_payment_id,
			amount_minor_units, currency, status, payment_method, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.Exec(ctx, q,
		p.ID, p.MerchantID, p.CustomerID, p.ExternalPaymentID,
		p.Amount.MinorUnits, string(p.Amount.Currency), string(p.Status), p.PaymentMethod,
		p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *PostgresPaymentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	const q = `
		SELECT id, merchant_id, customer_id, external_payment_id,
			amount_minor_units, currency, status, payment_method, created_at, updated_at
		FROM payments
		WHERE id = $1`
	var (
		p        domain.Payment
		currency string
		status   string
	)
	err := r.db.QueryRow(ctx, q, id).Scan(
		&p.ID, &p.MerchantID, &p.CustomerID, &p.ExternalPaymentID,
		&p.Amount.MinorUnits, &currency, &status, &p.PaymentMethod,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Amount.Currency = domain.Currency(currency)
	p.Status = domain.PaymentStatus(status)
	return &p, nil
}
