package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// RecoveryEventRepository persists and retrieves RecoveryEvent entities.
type RecoveryEventRepository interface {
	Create(ctx context.Context, e *domain.RecoveryEvent) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryEvent, error)
	// TryCreate inserts e unless a row with the same EventID already
	// exists, in which case it does nothing and reports created=false.
	// This is the durable idempotency guard for event ingestion: two
	// concurrent attempts to process the same event_id can both call
	// TryCreate, but only one will report created=true.
	TryCreate(ctx context.Context, e *domain.RecoveryEvent) (created bool, err error)
	// GetByEventID looks up a previously persisted event by its
	// caller-supplied EventID (as opposed to GetByID, which uses the
	// internal row ID). Used to answer "what happened last time we saw
	// this event_id" when TryCreate reports a duplicate.
	GetByEventID(ctx context.Context, eventID string) (*domain.RecoveryEvent, error)
	// SetRecoveryCaseID links a persisted event to the RecoveryCase it
	// was correlated to. Called once, after the case has been
	// created/located, within the same transaction as the event insert.
	SetRecoveryCaseID(ctx context.Context, id uuid.UUID, recoveryCaseID uuid.UUID) error
}

// PostgresRecoveryEventRepository is the PostgreSQL-backed
// RecoveryEventRepository.
type PostgresRecoveryEventRepository struct {
	db DBTX
}

func NewPostgresRecoveryEventRepository(db DBTX) *PostgresRecoveryEventRepository {
	return &PostgresRecoveryEventRepository{db: db}
}

func (r *PostgresRecoveryEventRepository) Create(ctx context.Context, e *domain.RecoveryEvent) error {
	const q = `
		INSERT INTO recovery_events (
			id, event_id, event_type, aggregate_type, aggregate_id,
			merchant_id, payload, occurred_at, created_at, recovery_case_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.Exec(ctx, q,
		e.ID, e.EventID, string(e.EventType), e.AggregateType, e.AggregateID,
		e.MerchantID, e.Payload, e.OccurredAt, e.CreatedAt, e.RecoveryCaseID)
	return err
}

func (r *PostgresRecoveryEventRepository) TryCreate(ctx context.Context, e *domain.RecoveryEvent) (bool, error) {
	const q = `
		INSERT INTO recovery_events (
			id, event_id, event_type, aggregate_type, aggregate_id,
			merchant_id, payload, occurred_at, created_at, recovery_case_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (event_id) DO NOTHING`
	tag, err := r.db.Exec(ctx, q,
		e.ID, e.EventID, string(e.EventType), e.AggregateType, e.AggregateID,
		e.MerchantID, e.Payload, e.OccurredAt, e.CreatedAt, e.RecoveryCaseID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresRecoveryEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryEvent, error) {
	const q = `
		SELECT id, event_id, event_type, aggregate_type, aggregate_id,
			merchant_id, payload, occurred_at, created_at, recovery_case_id
		FROM recovery_events
		WHERE id = $1`
	return r.scanOne(ctx, q, id)
}

func (r *PostgresRecoveryEventRepository) GetByEventID(ctx context.Context, eventID string) (*domain.RecoveryEvent, error) {
	const q = `
		SELECT id, event_id, event_type, aggregate_type, aggregate_id,
			merchant_id, payload, occurred_at, created_at, recovery_case_id
		FROM recovery_events
		WHERE event_id = $1`
	return r.scanOne(ctx, q, eventID)
}

func (r *PostgresRecoveryEventRepository) scanOne(ctx context.Context, q string, arg any) (*domain.RecoveryEvent, error) {
	var (
		e         domain.RecoveryEvent
		eventType string
	)
	err := r.db.QueryRow(ctx, q, arg).Scan(
		&e.ID, &e.EventID, &eventType, &e.AggregateType, &e.AggregateID,
		&e.MerchantID, &e.Payload, &e.OccurredAt, &e.CreatedAt, &e.RecoveryCaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.EventType = domain.RecoveryEventType(eventType)
	return &e, nil
}

func (r *PostgresRecoveryEventRepository) SetRecoveryCaseID(ctx context.Context, id uuid.UUID, recoveryCaseID uuid.UUID) error {
	const q = `UPDATE recovery_events SET recovery_case_id = $1 WHERE id = $2`
	_, err := r.db.Exec(ctx, q, recoveryCaseID, id)
	return err
}
