// Package http wires up the HTTP layer for the backend service.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRouter builds the chi router for the backend service. events,
// economicEvaluations, policyDecisions, executor, webhooks, and reconciler
// may be nil in tests that don't exercise the corresponding routes. pool
// may also be nil in tests that don't exercise the dashboard read routes
// (/v1/recovery-cases*, /v1/system-health) — those routes construct their
// own read-only repositories from pool at request time, never at router
// construction time, so a nil pool is safe until one of those routes is
// actually hit. aiServiceURL is used only by /v1/system-health's
// dependency check and may be empty (reported as "not configured").
func NewRouter(
	events eventProcessor,
	economicEvaluations economicEvaluationReader,
	policyDecisions policyDecisionReader,
	executor executionExecutor,
	webhooks webhookProcessor,
	reconciler reconciliationEngine,
	pool *pgxpool.Pool,
	aiServiceURL string,
) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/health", handleHealth)
	r.Get("/v1/evaluation", handleGetEvaluation)
	r.Get("/v1/system-health", handleGetSystemHealth(pool, aiServiceURL))
	r.Get("/v1/policies", handleGetPolicies)
	r.Get("/v1/recovery-cases", handleListRecoveryCases(pool))
	r.Get("/v1/recovery-cases/{id}", handleGetRecoveryCaseDetail(pool))
	r.Post("/events", handleCreateEvent(events))
	r.Get("/v1/recovery-cases/{id}/economic-evaluation", handleGetEconomicEvaluation(economicEvaluations))
	r.Get("/v1/recovery-cases/{id}/policy-decision", handleGetPolicyDecision(policyDecisions))
	r.Post("/v1/recovery-cases/{id}/execute", handleExecuteRecoveryCase(policyDecisions, executor))
	r.Post("/v1/webhooks/razorpay", handleRazorpayWebhook(webhooks))
	r.Post("/v1/recovery-cases/{id}/reconcile", handleReconcileRecoveryCase(reconciler))

	return r
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
