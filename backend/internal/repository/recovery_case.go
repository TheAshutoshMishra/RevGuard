package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
)

// RecoveryCaseRepository persists and retrieves RecoveryCase entities.
type RecoveryCaseRepository interface {
	Create(ctx context.Context, c *domain.RecoveryCase) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryCase, error)
	// GetOpenByPaymentID returns the non-CLOSED RecoveryCase for the given
	// payment, if any. The database enforces at most one such row per
	// payment (see migration 000011), so this is the correlation lookup
	// used to decide "create a new case" vs. "attach to the existing one"
	// for a qualifying revenue-risk event.
	GetOpenByPaymentID(ctx context.Context, paymentID uuid.UUID) (*domain.RecoveryCase, error)
	// UpdateStatus performs a guarded state transition: it only updates
	// the row if its current status still matches `from`, returning
	// ErrNotFound otherwise (the case moved, was deleted, or never had
	// that status). Callers should validate the transition with
	// service.ValidateTransition before calling this.
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to domain.RecoveryCaseStatus, now time.Time) error
	// List returns up to filter.Limit cases (most recently updated
	// first), optionally restricted to filter.Status, for the dashboard's
	// read-only case list (Milestone 11). Read-only — never used by any
	// engine.
	List(ctx context.Context, filter RecoveryCaseListFilter) ([]*domain.RecoveryCase, error)
	// Count returns how many cases match filter.Status (ignoring
	// filter.Limit/Offset), for the dashboard's pagination total.
	Count(ctx context.Context, filter RecoveryCaseListFilter) (int, error)
}

// RecoveryCaseListFilter narrows RecoveryCaseRepository.List/Count.
// Status == nil means "any status". Limit <= 0 defaults to 50 in the
// Postgres implementation; Offset < 0 is treated as 0.
type RecoveryCaseListFilter struct {
	Status *domain.RecoveryCaseStatus
	Limit  int
	Offset int
}

// PostgresRecoveryCaseRepository is the PostgreSQL-backed
// RecoveryCaseRepository.
type PostgresRecoveryCaseRepository struct {
	db DBTX
}

func NewPostgresRecoveryCaseRepository(db DBTX) *PostgresRecoveryCaseRepository {
	return &PostgresRecoveryCaseRepository{db: db}
}

func (r *PostgresRecoveryCaseRepository) Create(ctx context.Context, c *domain.RecoveryCase) error {
	const q = `
		INSERT INTO recovery_cases (
			id, merchant_id, customer_id, payment_id, status,
			revenue_at_risk_minor_units, currency, created_at, updated_at, closed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.Exec(ctx, q,
		c.ID, c.MerchantID, c.CustomerID, c.PaymentID, string(c.Status),
		c.RevenueAtRisk.MinorUnits, string(c.RevenueAtRisk.Currency),
		c.CreatedAt, c.UpdatedAt, c.ClosedAt)
	return err
}

func (r *PostgresRecoveryCaseRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecoveryCase, error) {
	const q = `
		SELECT id, merchant_id, customer_id, payment_id, status,
			revenue_at_risk_minor_units, currency, created_at, updated_at, closed_at
		FROM recovery_cases
		WHERE id = $1`
	var (
		c        domain.RecoveryCase
		status   string
		currency string
	)
	err := r.db.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.MerchantID, &c.CustomerID, &c.PaymentID, &status,
		&c.RevenueAtRisk.MinorUnits, &currency, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Status = domain.RecoveryCaseStatus(status)
	c.RevenueAtRisk.Currency = domain.Currency(currency)
	return &c, nil
}

func (r *PostgresRecoveryCaseRepository) GetOpenByPaymentID(ctx context.Context, paymentID uuid.UUID) (*domain.RecoveryCase, error) {
	const q = `
		SELECT id, merchant_id, customer_id, payment_id, status,
			revenue_at_risk_minor_units, currency, created_at, updated_at, closed_at
		FROM recovery_cases
		WHERE payment_id = $1 AND status <> 'CLOSED'`
	var (
		c        domain.RecoveryCase
		status   string
		currency string
	)
	err := r.db.QueryRow(ctx, q, paymentID).Scan(
		&c.ID, &c.MerchantID, &c.CustomerID, &c.PaymentID, &status,
		&c.RevenueAtRisk.MinorUnits, &currency, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Status = domain.RecoveryCaseStatus(status)
	c.RevenueAtRisk.Currency = domain.Currency(currency)
	return &c, nil
}

func (r *PostgresRecoveryCaseRepository) List(ctx context.Context, filter RecoveryCaseListFilter) ([]*domain.RecoveryCase, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	const baseQ = `
		SELECT id, merchant_id, customer_id, payment_id, status,
			revenue_at_risk_minor_units, currency, created_at, updated_at, closed_at
		FROM recovery_cases`

	var rows pgx.Rows
	var err error
	if filter.Status != nil {
		rows, err = r.db.Query(ctx, baseQ+` WHERE status = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`,
			string(*filter.Status), limit, offset)
	} else {
		rows, err = r.db.Query(ctx, baseQ+` ORDER BY updated_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cases []*domain.RecoveryCase
	for rows.Next() {
		var (
			c        domain.RecoveryCase
			status   string
			currency string
		)
		if err := rows.Scan(&c.ID, &c.MerchantID, &c.CustomerID, &c.PaymentID, &status,
			&c.RevenueAtRisk.MinorUnits, &currency, &c.CreatedAt, &c.UpdatedAt, &c.ClosedAt); err != nil {
			return nil, err
		}
		c.Status = domain.RecoveryCaseStatus(status)
		c.RevenueAtRisk.Currency = domain.Currency(currency)
		cases = append(cases, &c)
	}
	return cases, rows.Err()
}

func (r *PostgresRecoveryCaseRepository) Count(ctx context.Context, filter RecoveryCaseListFilter) (int, error) {
	var count int
	var err error
	if filter.Status != nil {
		err = r.db.QueryRow(ctx, `SELECT count(*) FROM recovery_cases WHERE status = $1`, string(*filter.Status)).Scan(&count)
	} else {
		err = r.db.QueryRow(ctx, `SELECT count(*) FROM recovery_cases`).Scan(&count)
	}
	return count, err
}

func (r *PostgresRecoveryCaseRepository) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domain.RecoveryCaseStatus, now time.Time) error {
	const q = `
		UPDATE recovery_cases
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status = $4`
	tag, err := r.db.Exec(ctx, q, string(to), now, id, string(from))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
