package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx. Repositories are
// constructed against a DBTX so the same repository code can run either
// directly against the pool or scoped to an explicit transaction (see
// backend/internal/service, which needs event persistence, recovery case
// creation, and audit writes to commit or roll back together).
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// IsUniqueViolation reports whether err is a PostgreSQL unique constraint
// violation (SQLSTATE 23505), e.g. from a racing INSERT against a unique
// index. Callers use this to distinguish "someone else already did this"
// from a genuine failure.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
