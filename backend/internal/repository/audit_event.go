package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
)

// AuditEventRepository persists and retrieves AuditEvent entities.
type AuditEventRepository interface {
	Create(ctx context.Context, e *domain.AuditEvent) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.AuditEvent, error)
}

// PostgresAuditEventRepository is the PostgreSQL-backed
// AuditEventRepository.
type PostgresAuditEventRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresAuditEventRepository(pool *pgxpool.Pool) *PostgresAuditEventRepository {
	return &PostgresAuditEventRepository{pool: pool}
}

func (r *PostgresAuditEventRepository) Create(ctx context.Context, e *domain.AuditEvent) error {
	const q = `
		INSERT INTO audit_events (
			id, recovery_case_id, event_type, actor_type, actor_id, metadata, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.pool.Exec(ctx, q,
		e.ID, e.RecoveryCaseID, e.EventType, string(e.ActorType), e.ActorID, e.Metadata, e.CreatedAt)
	return err
}

func (r *PostgresAuditEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.AuditEvent, error) {
	const q = `
		SELECT id, recovery_case_id, event_type, actor_type, actor_id, metadata, created_at
		FROM audit_events
		WHERE id = $1`
	var (
		e         domain.AuditEvent
		actorType string
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&e.ID, &e.RecoveryCaseID, &e.EventType, &actorType, &e.ActorID, &e.Metadata, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.ActorType = domain.AuditActorType(actorType)
	return &e, nil
}
