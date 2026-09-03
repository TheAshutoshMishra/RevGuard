// Package repository implements PostgreSQL persistence for domain entities.
// Repositories contain no business logic: they translate between domain
// structs and SQL rows only.
package repository

import "errors"

// ErrNotFound is returned when a lookup by ID finds no matching row.
var ErrNotFound = errors.New("repository: not found")
