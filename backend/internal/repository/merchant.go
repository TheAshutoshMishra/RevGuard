package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
)

// MerchantRepository persists and retrieves Merchant entities.
type MerchantRepository interface {
	Create(ctx context.Context, m *domain.Merchant) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Merchant, error)
}

// PostgresMerchantRepository is the PostgreSQL-backed MerchantRepository.
type PostgresMerchantRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresMerchantRepository(pool *pgxpool.Pool) *PostgresMerchantRepository {
	return &PostgresMerchantRepository{pool: pool}
}

func (r *PostgresMerchantRepository) Create(ctx context.Context, m *domain.Merchant) error {
	const q = `
		INSERT INTO merchants (id, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4)`
	_, err := r.pool.Exec(ctx, q, m.ID, m.Name, m.CreatedAt, m.UpdatedAt)
	return err
}

func (r *PostgresMerchantRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Merchant, error) {
	const q = `
		SELECT id, name, created_at, updated_at
		FROM merchants
		WHERE id = $1`
	var m domain.Merchant
	err := r.pool.QueryRow(ctx, q, id).Scan(&m.ID, &m.Name, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
