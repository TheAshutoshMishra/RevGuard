// Command migrate applies (or rolls back) the SQL migrations in
// backend/migrations against the configured PostgreSQL database.
//
// Usage:
//
//	go run ./cmd/migrate -command up
//	go run ./cmd/migrate -command down
//	go run ./cmd/migrate -command version
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"revguard/backend/internal/config"
)

func main() {
	command := flag.String("command", "up", "migration command: up, down, or version")
	path := flag.String("path", "migrations", "filesystem path to migration files")
	flag.Parse()

	cfg := config.Load()

	m, err := migrate.New("file://"+*path, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("migrate: failed to initialize: %v", err)
	}
	defer m.Close()

	switch *command {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	case "version":
		version, dirty, verr := m.Version()
		if verr != nil {
			log.Fatalf("migrate: failed to read version: %v", verr)
		}
		fmt.Printf("version=%d dirty=%v\n", version, dirty)
		return
	default:
		log.Fatalf("migrate: unknown command %q (want up, down, or version)", *command)
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("migrate: %s failed: %v", *command, err)
	}
	log.Printf("migrate: %s complete", *command)
}
