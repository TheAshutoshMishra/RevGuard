package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSystemHealth_NoPoolNoAIService(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/system-health", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Components []componentHealth `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	byName := map[string]componentHealth{}
	for _, c := range body.Components {
		byName[c.Name] = c
	}

	if byName["Go API"].Status != "UP" {
		t.Fatalf("expected Go API UP, got %+v", byName["Go API"])
	}
	if byName["PostgreSQL"].Status != "NOT_CONFIGURED" {
		t.Fatalf("expected PostgreSQL NOT_CONFIGURED with a nil pool, got %+v", byName["PostgreSQL"])
	}
	if byName["AI Service"].Status != "NOT_CONFIGURED" {
		t.Fatalf("expected AI Service NOT_CONFIGURED with an empty URL, got %+v", byName["AI Service"])
	}
	if byName["Redis"].Status != "NOT_CONFIGURED" {
		t.Fatalf("expected Redis NOT_CONFIGURED (never claim healthy without a real check), got %+v", byName["Redis"])
	}
	if byName["Redpanda"].Status != "NOT_CONFIGURED" {
		t.Fatalf("expected Redpanda NOT_CONFIGURED (never claim healthy without a real check), got %+v", byName["Redpanda"])
	}
}

func TestGetSystemHealth_AIServiceUnreachableReportsDown(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/system-health", nil)
	rec := httptest.NewRecorder()

	// Port 1 is reliably unreachable (no listener) — proves an
	// unreachable AI service is reported DOWN, not silently omitted or
	// fabricated as UP.
	handleGetSystemHealth(nil, "http://127.0.0.1:1")(rec, req)

	var body struct {
		Components []componentHealth `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	for _, c := range body.Components {
		if c.Name == "AI Service" {
			if c.Status != "DOWN" {
				t.Fatalf("expected AI Service DOWN when unreachable, got %+v", c)
			}
			return
		}
	}
	t.Fatal("AI Service component missing from response")
}
