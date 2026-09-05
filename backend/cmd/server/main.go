// Command server is the entry point for the RevGuard backend API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"revguard/backend/internal/config"
	revguardhttp "revguard/backend/internal/http"
	"revguard/backend/internal/infrastructure"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

// shutdownGracePeriod bounds how long the server waits for in-flight
// requests to finish after receiving a shutdown signal before forcing
// the process to exit — long enough for a real Razorpay API call
// (execution/reconciliation) in flight to complete, short enough that a
// deploy or restart isn't left hanging indefinitely.
const shutdownGracePeriod = 25 * time.Second

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := infrastructure.NewPostgresPool(ctx, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	// Closed explicitly, once, at the end of main — after Shutdown has
	// drained in-flight requests, not via defer (which would race the
	// explicit pool.Close() call at the end of this function otherwise).

	publisher := service.NewLoggingEventPublisher(nil)

	aiClient := service.NewHTTPAIClient(cfg.AIServiceURL, cfg.AIRequestTimeout, nil)
	contextBuilder := service.NewRecoveryContextBuilder(
		repository.NewPostgresPaymentRepository(pool),
		repository.NewPostgresPaymentAttemptRepository(pool),
		repository.NewPostgresRecoveryActionRepository(pool),
	)
	analyzer := service.NewAnalysisOrchestrator(pool, contextBuilder, aiClient, nil)

	economicEngine := service.NewEconomicEngine(pool, service.NewHeuristicProbabilityEstimator(), nil)

	policyConfig, ok := service.PolicyProfiles[cfg.PolicyProfile]
	if !ok {
		log.Fatalf("unknown POLICY_PROFILE %q (expected one of: conservative, balanced, aggressive)", cfg.PolicyProfile)
	}
	policyEngine := service.NewPolicyEngine(pool, policyConfig, nil)

	processor := service.NewEventProcessor(pool, analyzer, economicEngine, policyEngine, publisher, nil)

	paymentProvider, err := buildPaymentProvider(cfg)
	if err != nil {
		log.Fatalf("failed to build payment provider: %v", err)
	}
	executionEngine := service.NewExecutionEngine(pool, paymentProvider, nil)

	webhookVerifier := service.NewConfiguredWebhookVerifier(cfg.RazorpayWebhookSecret)
	webhookParser := service.NewRazorpayWebhookParser()
	webhookProcessor := service.NewWebhookProcessor(pool, webhookVerifier, webhookParser, nil)

	paymentReconciler, err := buildPaymentReconciler(cfg)
	if err != nil {
		log.Fatalf("failed to build payment reconciler: %v", err)
	}
	reconciliationEngine := service.NewReconciliationEngine(pool, paymentReconciler, nil)

	router := revguardhttp.NewRouter(processor, economicEngine, policyEngine, executionEngine, webhookProcessor, reconciliationEngine, pool, cfg.AIServiceURL)

	addr := ":" + cfg.BackendPort
	srv := &http.Server{Addr: addr, Handler: router}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("revguard backend listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	// Block until either a shutdown signal (SIGINT/SIGTERM, e.g. from a
	// container orchestrator during a deploy/restart) arrives or the
	// server itself fails to start/serve.
	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received, draining in-flight requests (up to %s)", shutdownGracePeriod)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown did not complete cleanly: %v", err)
		}
	case err := <-serveErr:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
	}

	log.Print("closing database pool")
	pool.Close()
	log.Print("revguard backend stopped")
}

// buildPaymentProvider selects the ExecutionEngine's PaymentProvider based
// on config.PaymentProvider. Defaults to the fake provider, which makes no
// external network calls and is safe for local development — a
// misconfigured "razorpay" selection without credentials fails fast at
// startup rather than silently falling back to fake.
func buildPaymentProvider(cfg config.Config) (service.PaymentProvider, error) {
	switch cfg.PaymentProvider {
	case "", "fake":
		return service.NewFakeProvider(service.FakeProviderScenarioSuccess), nil
	case "razorpay":
		return service.NewRazorpayProvider(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayBaseURL, nil)
	default:
		return nil, fmt.Errorf("unknown PAYMENT_PROVIDER %q (expected \"fake\" or \"razorpay\")", cfg.PaymentProvider)
	}
}

// buildPaymentReconciler selects the ReconciliationEngine's
// PaymentReconciler based on config.PaymentProvider — the same selector
// buildPaymentProvider uses, since a case executed against a given
// provider must be reconciled against that same provider. Defaults to a
// fixed-scenario FakeReconciler (RECONCILER_FAKE_SCENARIO, safe for
// local/dev — no external network calls); "payment_captured" with a zero
// default amount is deliberately inert (never fabricates a SUCCESS
// outcome) unless RECONCILER_FAKE_AMOUNT_MINOR_UNITS is explicitly set,
// consistent with never guessing a financial outcome.
func buildPaymentReconciler(cfg config.Config) (service.PaymentReconciler, error) {
	switch cfg.PaymentProvider {
	case "", "fake":
		scenario := service.FakeReconcilerScenario(cfg.FakeReconcilerScenario)
		return service.NewFakeReconciler(scenario, cfg.FakeReconcilerAmount, cfg.FakeReconcilerCurrency), nil
	case "razorpay":
		return service.NewRazorpayReconciler(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayBaseURL, nil)
	default:
		return nil, fmt.Errorf("unknown PAYMENT_PROVIDER %q (expected \"fake\" or \"razorpay\")", cfg.PaymentProvider)
	}
}
