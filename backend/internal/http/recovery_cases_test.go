package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRecoveryCases_NoPoolReturns503(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with no pool configured, got %d", rec.Code)
	}
}

func TestListRecoveryCases_InvalidStatusRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases?status=NOT_A_REAL_STATUS", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid status, got %d", rec.Code)
	}
}

func TestListRecoveryCases_InvalidLimitRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases?limit=-5", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a negative limit, got %d", rec.Code)
	}
}

func TestGetRecoveryCaseDetail_NoPoolReturns503(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases/00000000-0000-0000-0000-000000000000", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with no pool configured, got %d", rec.Code)
	}
}
