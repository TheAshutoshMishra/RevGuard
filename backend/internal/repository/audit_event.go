package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// AuditEventRepository persists and retrieves AuditEvent entities.
type AuditEventRepository interface {
	Create(ctx context.Context, e *domain.AuditEvent) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.AuditEvent, error)
	// ListByRecoveryCaseID returns every audit event for a case in
	// chronological order (oldest first) — the durable audit trail
	// displayed on the dashboard's case detail page (Milestone 11).
	ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.AuditEvent, error)
}

// PostgresAuditEventRepository is the PostgreSQL-backed
// AuditEventRepository.
type PostgresAuditEventRepository struct {
	db DBTX
}

func NewPostgresAuditEventRepository(db DBTX) *PostgresAuditEventRepository {
	return &PostgresAuditEventRepository{db: db}
}

func (r *PostgresAuditEventRepository) Create(ctx context.Context, e *domain.AuditEvent) error {
	const q = `
		INSERT INTO audit_events (
			id, recovery_case_id, event_type, actor_type, actor_id, metadata, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.Exec(ctx, q,
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
	err := r.db.QueryRow(ctx, q, id).Scan(
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

func (r *PostgresAuditEventRepository) ListByRecoveryCaseID(ctx context.Context, recoveryCaseID uuid.UUID) ([]*domain.AuditEvent, error) {
	const q = `
		SELECT id, recovery_case_id, event_type, actor_type, actor_id, metadata, created_at
		FROM audit_events
		WHERE recovery_case_id = $1
		ORDER BY created_at ASC`
	rows, err := r.db.Query(ctx, q, recoveryCaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*domain.AuditEvent
	for rows.Next() {
		var (
			e         domain.AuditEvent
			actorType string
		)
		if err := rows.Scan(&e.ID, &e.RecoveryCaseID, &e.EventType, &actorType, &e.ActorID, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ActorType = domain.AuditActorType(actorType)
		events = append(events, &e)
	}
	return events, rows.Err()
}
