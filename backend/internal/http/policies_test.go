package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetPolicies_ReturnsAllThreeProfiles(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/policies", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Profiles []policyProfileResponse `json:"profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(body.Profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(body.Profiles))
	}

	keys := map[string]bool{}
	for _, p := range body.Profiles {
		keys[p.Key] = true
		if p.MinimumConfidence <= 0 {
			t.Fatalf("%s: expected a positive minimum confidence", p.Key)
		}
		if len(p.AutoAllowedActions) == 0 {
			t.Fatalf("%s: expected at least one auto-allowed action", p.Key)
		}
	}
	for _, want := range []string{"conservative", "balanced", "aggressive"} {
		if !keys[want] {
			t.Fatalf("missing profile %q", want)
		}
	}
}

// TestGetPolicies_NoMutationRoute confirms no write route exists for
// policy configuration anywhere in the router — a POST/PUT to the same
// path must not be routed to a handler that could change policy.
func TestGetPolicies_NoMutationRoute(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/v1/policies", nil)
		rec := httptest.NewRecorder()
		NewRouter(nil, nil, nil, nil, nil, nil, nil, "").ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s /v1/policies unexpectedly succeeded — policy must be read-only", method)
		}
	}
}
