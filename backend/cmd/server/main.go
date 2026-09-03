// Command server is the entry point for the RevGuard backend API.
package main

import (
	"context"
	"log"
	"net/http"

	"revguard/backend/internal/config"
	revguardhttp "revguard/backend/internal/http"
	"revguard/backend/internal/infrastructure"
	"revguard/backend/internal/service"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	pool, err := infrastructure.NewPostgresPool(ctx, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	publisher := service.NewLoggingEventPublisher(nil)
	processor := service.NewEventProcessor(pool, publisher, nil)

	router := revguardhttp.NewRouter(processor)

	addr := ":" + cfg.BackendPort
	log.Printf("revguard backend listening on %s", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
