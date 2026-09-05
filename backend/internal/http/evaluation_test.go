package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"revguard/backend/internal/service"
)

func TestGetEvaluation_Defaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/evaluation", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result service.EvaluationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Dataset.Seed != 12345 || result.Dataset.Opportunities != 1000 {
		t.Fatalf("expected default seed=12345 cases=1000, got seed=%d cases=%d", result.Dataset.Seed, result.Dataset.Opportunities)
	}
	if result.Dataset.Type != "synthetic" {
		t.Fatalf("expected dataset type synthetic, got %q", result.Dataset.Type)
	}
}

func TestGetEvaluation_CustomSeedAndCases(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/evaluation?seed=42&cases=50", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var result service.EvaluationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Dataset.Seed != 42 || result.Dataset.Opportunities != 50 {
		t.Fatalf("expected seed=42 cases=50, got seed=%d cases=%d", result.Dataset.Seed, result.Dataset.Opportunities)
	}
}

func TestGetEvaluation_InvalidSeedRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/evaluation?seed=notanumber", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestGetEvaluation_CasesAboveMaxRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/evaluation?cases=999999", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestGetEvaluation_NegativeCasesRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/evaluation?cases=-1", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}
