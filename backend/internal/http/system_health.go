package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// componentHealth's Status is one of "UP", "DOWN", or "NOT_CONFIGURED" —
// never fabricated as "UP" merely because this HTTP process is running.
type componentHealth struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	CheckedAt string `json:"checked_at"`
}

// handleGetSystemHealth is a read-only dashboard endpoint (Milestone
// 11): GET /v1/system-health. It performs a real check for every
// component this backend actually has a client for (PostgreSQL via
// pool.Ping, the AI service via a real HTTP GET to its /health
// endpoint), and reports NOT_CONFIGURED — never a fabricated UP — for
// Redis and Redpanda, which no Go code in this codebase has ever
// connected to (see internal/infrastructure: only postgres.go exists).
// Redis/Redpanda are declared in docker-compose per the locked
// architecture but, per every prior milestone's CLAUDE.md notes, no
// engine has used them yet.
func handleGetSystemHealth(pool *pgxpool.Pool, aiServiceURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC().Format(timeFormat)
		components := []componentHealth{
			checkGoAPI(now),
			checkPostgres(r.Context(), pool, now),
			checkAIService(r.Context(), aiServiceURL, now),
			{Name: "Redis", Status: "NOT_CONFIGURED", Detail: "declared in docker-compose, not used by any Go code path yet (idempotency/cache is not exercised by any milestone so far)", CheckedAt: now},
			{Name: "Redpanda", Status: "NOT_CONFIGURED", Detail: "declared in docker-compose, not used by any Go code path yet (LoggingEventPublisher logs events instead of publishing to a broker)", CheckedAt: now},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"components": components, "checked_at": now})
	}
}

func checkGoAPI(now string) componentHealth {
	// If this handler is running, the Go API that's serving this exact
	// request is, by definition, up — this is the one component allowed
	// to be reported UP without an external round-trip.
	return componentHealth{Name: "Go API", Status: "UP", CheckedAt: now}
}

func checkPostgres(ctx context.Context, pool *pgxpool.Pool, now string) componentHealth {
	if pool == nil {
		return componentHealth{Name: "PostgreSQL", Status: "NOT_CONFIGURED", Detail: "no database pool wired into this server process", CheckedAt: now}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return componentHealth{Name: "PostgreSQL", Status: "DOWN", Detail: err.Error(), CheckedAt: now}
	}
	return componentHealth{Name: "PostgreSQL", Status: "UP", CheckedAt: now}
}

func checkAIService(ctx context.Context, aiServiceURL string, now string) componentHealth {
	if aiServiceURL == "" {
		return componentHealth{Name: "AI Service", Status: "NOT_CONFIGURED", Detail: "AI_SERVICE_URL is not set", CheckedAt: now}
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, aiServiceURL+"/health", nil)
	if err != nil {
		return componentHealth{Name: "AI Service", Status: "DOWN", Detail: "failed to build health request", CheckedAt: now}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return componentHealth{Name: "AI Service", Status: "DOWN", Detail: "unreachable: " + err.Error(), CheckedAt: now}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return componentHealth{Name: "AI Service", Status: "DEGRADED", Detail: "unexpected status from /health", CheckedAt: now}
	}
	return componentHealth{Name: "AI Service", Status: "UP", CheckedAt: now}
}
