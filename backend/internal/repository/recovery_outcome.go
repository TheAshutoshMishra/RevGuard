package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// RecoveryOutcomeRepository persists and retrieves RecoveryOutcome entities.
type RecoveryOutcomeRepository interface {
	Create(ctx context.Context, o *domain.RecoveryOutcome) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryOutcome, error)
	// GetByRecoveryActionID looks up the (at most one, per
	// UNIQUE(recovery_action_id) — migration 000017) outcome for a given
	// execution attempt.
	GetByRecoveryActionID(ctx context.Context, recoveryActionID uuid.UUID) (*domain.RecoveryOutcome, error)
	// TryCreate inserts o unless an outcome for the same
	// RecoveryActionID already exists (ON CONFLICT DO NOTHING),
	// reporting created=false in that case. Never errors on conflict, so
	// it never poisons the enclosing transaction — the same idempotent
	// pattern as RecoveryEconomicEvaluationRepository.TryCreate.
	TryCreate(ctx context.Context, o *domain.RecoveryOutcome) (created bool, err error)
}

// PostgresRecoveryOutcomeRepository is the PostgreSQL-backed
// RecoveryOutcomeRepository.
type PostgresRecoveryOutcomeRepository struct {
	db DBTX
}

func NewPostgresRecoveryOutcomeRepository(db DBTX) *PostgresRecoveryOutcomeRepository {
	return &PostgresRecoveryOutcomeRepository{db: db}
}

func (r *PostgresRecoveryOutcomeRepository) Create(ctx context.Context, o *domain.RecoveryOutcome) error {
	const q = `
		INSERT INTO recovery_outcomes (
			id, recovery_case_id, recovery_action_id, status,
			recovered_amount_minor_units, currency, external_reference, observed_at, created_at,
			provider, source, provider_webhook_event_id, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	_, err := r.db.Exec(ctx, q,
		o.ID, o.RecoveryCaseID, o.RecoveryActionID, string(o.Status),
		o.RecoveredAmount.MinorUnits, string(o.RecoveredAmount.Currency),
		nullableString(o.ExternalReference), o.ObservedAt, o.CreatedAt,
		nullableString(o.Provider), nullableSource(o.Source), o.ProviderWebhookEventID, nonNilJSON(o.Metadata))
	return err
}

func (r *PostgresRecoveryOutcomeRepository) TryCreate(ctx context.Context, o *domain.RecoveryOutcome) (bool, error) {
	const q = `
		INSERT INTO recovery_outcomes (
			id, recovery_case_id, recovery_action_id, status,
			recovered_amount_minor_units, currency, external_reference, observed_at, created_at,
			provider, source, provider_webhook_event_id, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (recovery_action_id) DO NOTHING`
	tag, err := r.db.Exec(ctx, q,
		o.ID, o.RecoveryCaseID, o.RecoveryActionID, string(o.Status),
		o.RecoveredAmount.MinorUnits, string(o.RecoveredAmount.Currency),
		nullableString(o.ExternalReference), o.ObservedAt, o.CreatedAt,
		nullableString(o.Provider), nullableSource(o.Source), o.ProviderWebhookEventID, nonNilJSON(o.Metadata))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresRecoveryOutcomeRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryOutcome, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_action_id, status,
			recovered_amount_minor_units, currency, external_reference, observed_at, created_at,
			provider, source, provider_webhook_event_id, metadata
		FROM recovery_outcomes
		WHERE id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, id))
}

func (r *PostgresRecoveryOutcomeRepository) GetByRecoveryActionID(ctx context.Context, recoveryActionID uuid.UUID) (*domain.RecoveryOutcome, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_action_id, status,
			recovered_amount_minor_units, currency, external_reference, observed_at, created_at,
			provider, source, provider_webhook_event_id, metadata
		FROM recovery_outcomes
		WHERE recovery_action_id = $1`
	return r.scanOne(r.db.QueryRow(ctx, q, recoveryActionID))
}

func (r *PostgresRecoveryOutcomeRepository) scanOne(row rowScanner) (*domain.RecoveryOutcome, error) {
	var (
		o        domain.RecoveryOutcome
		status   string
		currency string

		externalReference *string
		provider          *string
		source            *string
	)
	err := row.Scan(
		&o.ID, &o.RecoveryCaseID, &o.RecoveryActionID, &status,
		&o.RecoveredAmount.MinorUnits, &currency, &externalReference, &o.ObservedAt, &o.CreatedAt,
		&provider, &source, &o.ProviderWebhookEventID, &o.Metadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Status = domain.RecoveryOutcomeStatus(status)
	o.RecoveredAmount.Currency = domain.Currency(currency)
	if externalReference != nil {
		o.ExternalReference = *externalReference
	}
	if provider != nil {
		o.Provider = *provider
	}
	if source != nil {
		o.Source = domain.RecoveryOutcomeSource(*source)
	}
	return &o, nil
}

func nullableSource(s domain.RecoveryOutcomeSource) *string {
	if s == "" {
		return nil
	}
	v := string(s)
	return &v
}
