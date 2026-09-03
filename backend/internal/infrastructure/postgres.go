// Package infrastructure holds thin wrappers around external systems
// (PostgreSQL, Redis, Redpanda). No business logic belongs here.
package infrastructure

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates a pgx connection pool for the given DSN.
// Callers are responsible for closing the pool.
func NewPostgresPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}
