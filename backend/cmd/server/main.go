// Command server is the entry point for the RevGuard backend API.
package main

import (
	"context"
	"fmt"
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

	policyEngine := service.NewPolicyEngine(pool, service.DefaultPolicyConfig, nil)

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

	router := revguardhttp.NewRouter(processor, economicEngine, policyEngine, executionEngine, webhookProcessor, reconciliationEngine)

	addr := ":" + cfg.BackendPort
	log.Printf("revguard backend listening on %s", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
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
