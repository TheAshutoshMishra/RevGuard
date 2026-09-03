package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// ProviderWebhookEventRepository persists and retrieves ProviderWebhookEvent
// entities — the durable idempotency authority for inbound provider
// webhooks (Milestone 7).
type ProviderWebhookEventRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.ProviderWebhookEvent, error)
	// GetByProviderEventID looks up the (at most one, per
	// UNIQUE(provider, provider_event_id)) event for a given provider
	// notification.
	GetByProviderEventID(ctx context.Context, provider, providerEventID string) (*domain.ProviderWebhookEvent, error)
	// TryCreate inserts e unless an event with the same
	// (provider, provider_event_id) already exists (ON CONFLICT DO
	// NOTHING), reporting created=false in that case — the idempotency
	// guard for at-least-once webhook delivery. Never errors on
	// conflict, so it never poisons the enclosing transaction.
	TryCreate(ctx context.Context, e *domain.ProviderWebhookEvent) (created bool, err error)
}

// PostgresProviderWebhookEventRepository is the PostgreSQL-backed
// ProviderWebhookEventRepository.
type PostgresProviderWebhookEventRepository struct {
	db DBTX
}

func NewPostgresProviderWebhookEventRepository(db DBTX) *PostgresProviderWebhookEventRepository {
	return &PostgresProviderWebhookEventRepository{db: db}
}

func (r *PostgresProviderWebhookEventRepository) TryCreate(ctx context.Context, e *domain.ProviderWebhookEvent) (bool, error) {
	const q = `
		INSERT INTO provider_webhook_events (
			id, provider, provider_event_id, event_type, provider_reference, status,
			amount_minor_units, currency, occurred_at, recovery_action_id, matched,
			metadata, received_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (provider, provider_event_id) DO NOTHING`
	tag, err := r.db.Exec(ctx, q,
		e.ID, e.Provider, e.ProviderEventID, e.EventType, nullableString(e.ProviderReference), string(e.Status),
		e.AmountMinorUnits, nullableCurrency(e.Currency), e.OccurredAt, e.RecoveryActionID, e.Matched,
		nonNilJSON(e.Metadata), e.ReceivedAt, e.CreatedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresProviderWebhookEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.ProviderWebhookEvent, error) {
	const q = `
		SELECT id, provider, provider_event_id, event_type, provider_reference, status,
			amount_minor_units, currency, occurred_at, recovery_action_id, matched,
			metadata, received_at, created_at
		FROM provider_webhook_events
		WHERE id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, id))
}

func (r *PostgresProviderWebhookEventRepository) GetByProviderEventID(ctx context.Context, provider, providerEventID string) (*domain.ProviderWebhookEvent, error) {
	const q = `
		SELECT id, provider, provider_event_id, event_type, provider_reference, status,
			amount_minor_units, currency, occurred_at, recovery_action_id, matched,
			metadata, received_at, created_at
		FROM provider_webhook_events
		WHERE provider = $1 AND provider_event_id = $2`
	return r.scanOne(r.db.QueryRow(ctx, q, provider, providerEventID))
}

func (r *PostgresProviderWebhookEventRepository) scanOne(row rowScanner) (*domain.ProviderWebhookEvent, error) {
	var (
		e                 domain.ProviderWebhookEvent
		status            string
		providerReference *string
		currency          *string
	)
	err := row.Scan(
		&e.ID, &e.Provider, &e.ProviderEventID, &e.EventType, &providerReference, &status,
		&e.AmountMinorUnits, &currency, &e.OccurredAt, &e.RecoveryActionID, &e.Matched,
		&e.Metadata, &e.ReceivedAt, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Status = domain.ProviderEventStatus(status)
	if providerReference != nil {
		e.ProviderReference = *providerReference
	}
	if currency != nil {
		e.Currency = domain.Currency(*currency)
	}
	return &e, nil
}

// nullableCurrency converts an empty Go domain.Currency to a SQL NULL,
// following the same convention as nullableString.
func nullableCurrency(c domain.Currency) *string {
	if c == "" {
		return nil
	}
	s := string(c)
	return &s
}
