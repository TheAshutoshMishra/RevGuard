package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// CustomerRepository persists and retrieves Customer entities.
type CustomerRepository interface {
	Create(ctx context.Context, c *domain.Customer) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Customer, error)
}

// PostgresCustomerRepository is the PostgreSQL-backed CustomerRepository.
type PostgresCustomerRepository struct {
	db DBTX
}

func NewPostgresCustomerRepository(db DBTX) *PostgresCustomerRepository {
	return &PostgresCustomerRepository{db: db}
}

func (r *PostgresCustomerRepository) Create(ctx context.Context, c *domain.Customer) error {
	const q = `
		INSERT INTO customers (id, merchant_id, external_customer_id, email, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.Exec(ctx, q,
		c.ID, c.MerchantID, c.ExternalCustomerID, c.Email, c.Name, c.CreatedAt, c.UpdatedAt)
	return err
}

func (r *PostgresCustomerRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Customer, error) {
	const q = `
		SELECT id, merchant_id, external_customer_id, email, name, created_at, updated_at
		FROM customers
		WHERE id = $1`
	var c domain.Customer
	err := r.db.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.MerchantID, &c.ExternalCustomerID, &c.Email, &c.Name, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
