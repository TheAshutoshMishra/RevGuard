package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCORSHeaderPresent guards the exact regression this milestone
// fixed: a browser fetch() from the frontend's origin to this API
// previously failed with a CORS error (network-level "Failed to fetch")
// because no Access-Control-Allow-Origin header was ever set.
func TestCORSHeaderPresent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:3199")
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Fatal("expected Access-Control-Allow-Origin header to be set")
	}
}

func TestCORSPreflightHandled(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/v1/evaluation", nil)
	req.Header.Set("Origin", "http://localhost:3199")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for an OPTIONS preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Fatal("expected Access-Control-Allow-Origin header on the preflight response")
	}
}
