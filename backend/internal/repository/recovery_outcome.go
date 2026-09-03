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
			recovered_amount_minor_units, currency, external_reference, observed_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.Exec(ctx, q,
		o.ID, o.RecoveryCaseID, o.RecoveryActionID, string(o.Status),
		o.RecoveredAmount.MinorUnits, string(o.RecoveredAmount.Currency),
		o.ExternalReference, o.ObservedAt, o.CreatedAt)
	return err
}

func (r *PostgresRecoveryOutcomeRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryOutcome, error) {
	const q = `
		SELECT id, recovery_case_id, recovery_action_id, status,
			recovered_amount_minor_units, currency, external_reference, observed_at, created_at
		FROM recovery_outcomes
		WHERE id = $1`
	var (
		o        domain.RecoveryOutcome
		status   string
		currency string
	)
	err := r.db.QueryRow(ctx, q, id).Scan(
		&o.ID, &o.RecoveryCaseID, &o.RecoveryActionID, &status,
		&o.RecoveredAmount.MinorUnits, &currency, &o.ExternalReference, &o.ObservedAt, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Status = domain.RecoveryOutcomeStatus(status)
	o.RecoveredAmount.Currency = domain.Currency(currency)
	return &o, nil
}
