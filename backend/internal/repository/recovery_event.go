package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
)

// RecoveryEventRepository persists and retrieves RecoveryEvent entities.
type RecoveryEventRepository interface {
	Create(ctx context.Context, e *domain.RecoveryEvent) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryEvent, error)
}

// PostgresRecoveryEventRepository is the PostgreSQL-backed
// RecoveryEventRepository.
type PostgresRecoveryEventRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRecoveryEventRepository(pool *pgxpool.Pool) *PostgresRecoveryEventRepository {
	return &PostgresRecoveryEventRepository{pool: pool}
}

func (r *PostgresRecoveryEventRepository) Create(ctx context.Context, e *domain.RecoveryEvent) error {
	const q = `
		INSERT INTO recovery_events (
			id, event_id, event_type, aggregate_type, aggregate_id,
			merchant_id, payload, occurred_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.pool.Exec(ctx, q,
		e.ID, e.EventID, string(e.EventType), e.AggregateType, e.AggregateID,
		e.MerchantID, e.Payload, e.OccurredAt, e.CreatedAt)
	return err
}

func (r *PostgresRecoveryEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryEvent, error) {
	const q = `
		SELECT id, event_id, event_type, aggregate_type, aggregate_id,
			merchant_id, payload, occurred_at, created_at
		FROM recovery_events
		WHERE id = $1`
	var (
		e         domain.RecoveryEvent
		eventType string
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&e.ID, &e.EventID, &eventType, &e.AggregateType, &e.AggregateID,
		&e.MerchantID, &e.Payload, &e.OccurredAt, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.EventType = domain.RecoveryEventType(eventType)
	return &e, nil
}
