package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"revguard/backend/internal/service"
)

// evaluationMaxCases bounds the ?cases= query parameter so this
// read-only, unauthenticated endpoint can't be used to force an
// arbitrarily large in-process computation.
const evaluationMaxCases = 5000

// handleGetEvaluation is a minimal, read-only endpoint (Milestone 10):
// GET /v1/evaluation?seed=12345&cases=1000. It runs RevGuard's
// deterministic, offline evaluation (service.RunEvaluation) and returns
// the result as-is — the exact same SYNTHETIC data
// docs/architecture/evaluation-engine.md describes, never real
// Razorpay/merchant data, never hand-edited or hardcoded. There is
// nothing to authorize or execute here: this endpoint opens no database
// connection, makes no external call, and cannot affect any
// RecoveryCase.
func handleGetEvaluation(w http.ResponseWriter, r *http.Request) {
	seed := int64(12345)
	if v := r.URL.Query().Get("seed"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid seed")
			return
		}
		seed = parsed
	}

	cases := 1000
	if v := r.URL.Query().Get("cases"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid cases")
			return
		}
		cases = parsed
	}
	if cases > evaluationMaxCases {
		writeError(w, http.StatusBadRequest, "cases exceeds the maximum allowed for this endpoint")
		return
	}

	result, err := service.RunEvaluation(seed, cases)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to run evaluation")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
