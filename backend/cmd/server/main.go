// Command server is the entry point for the RevGuard backend API.
package main

import (
	"context"
	"log"
	"net/http"

	"revguard/backend/internal/config"
	revguardhttp "revguard/backend/internal/http"
	"revguard/backend/internal/infrastructure"
	"revguard/backend/internal/repository"
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

	aiClient := service.NewHTTPAIClient(cfg.AIServiceURL, cfg.AIRequestTimeout, nil)
	contextBuilder := service.NewRecoveryContextBuilder(
		repository.NewPostgresPaymentRepository(pool),
		repository.NewPostgresPaymentAttemptRepository(pool),
		repository.NewPostgresRecoveryActionRepository(pool),
	)
	analyzer := service.NewAnalysisOrchestrator(pool, contextBuilder, aiClient, nil)

	economicEngine := service.NewEconomicEngine(pool, service.NewHeuristicProbabilityEstimator(), nil)

	processor := service.NewEventProcessor(pool, analyzer, economicEngine, publisher, nil)

	router := revguardhttp.NewRouter(processor, economicEngine)

	addr := ":" + cfg.BackendPort
	log.Printf("revguard backend listening on %s", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
