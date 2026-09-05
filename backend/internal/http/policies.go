package http

import (
	"encoding/json"
	"net/http"
	"sort"

	"revguard/backend/internal/service"
)

type policyProfileResponse struct {
	Key                                       string   `json:"key"`
	Version                                   string   `json:"version"`
	MinimumConfidence                         float64  `json:"minimum_confidence"`
	MaxAutoAmountMinorUnits                   int64    `json:"max_auto_amount_minor_units"`
	MinimumExpectedIncrementalValueMinorUnits int64    `json:"minimum_expected_incremental_value_minor_units"`
	MaxPaymentAttempts                        int      `json:"max_payment_attempts"`
	MaxPriorRecoveryActions                   int      `json:"max_prior_recovery_actions"`
	AutoAllowedActions                        []string `json:"auto_allowed_actions"`
}

// handleGetPolicies is a read-only dashboard endpoint (Milestone 11):
// GET /v1/policies. It returns the exact PolicyConfig values
// PolicyEngine can be configured with (service.PolicyProfiles) — pure,
// static, in-process data, no database call. There is no corresponding
// write endpoint: policy configuration is a deployment-time choice
// (POLICY_PROFILE env var), never a runtime API mutation, so this page
// can never be used to bypass a safety control.
func handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	order := []string{service.PolicyProfileConservative, service.PolicyProfileBalanced, service.PolicyProfileAggressive}

	profiles := make([]policyProfileResponse, 0, len(order))
	for _, key := range order {
		config, ok := service.PolicyProfiles[key]
		if !ok {
			continue
		}
		var allowed []string
		for action, isAllowed := range config.AutoAllowedActions {
			if isAllowed {
				allowed = append(allowed, string(action))
			}
		}
		sort.Strings(allowed)

		profiles = append(profiles, policyProfileResponse{
			Key: key, Version: config.Version, MinimumConfidence: config.MinimumConfidence,
			MaxAutoAmountMinorUnits:                   config.MaxAutoAmountMinorUnits,
			MinimumExpectedIncrementalValueMinorUnits: config.MinimumExpectedIncrementalValueMinorUnits,
			MaxPaymentAttempts:                        config.MaxPaymentAttempts,
			MaxPriorRecoveryActions:                   config.MaxPriorRecoveryActions,
			AutoAllowedActions:                        allowed,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"profiles": profiles,
		"currency": "INR",
		"note":     "Policy is deterministic and read-only here. AI recommendations never authorize execution; PolicyEngine's rules cannot be bypassed from this API.",
	})
}
