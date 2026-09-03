package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// policyDecisionReader is the subset of service.PolicyEngine's API this
// handler needs. Defined at the point of use so the handler can be
// exercised with a fake in tests without a real database.
type policyDecisionReader interface {
	GetLatestDecision(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.PolicyDecision, error)
}

// handleGetPolicyDecision is a minimal, read-only verification endpoint:
// GET /v1/recovery-cases/{id}/policy-decision. It exposes no way to
// approve, execute, or override a decision — the Policy Engine only
// decides, and this endpoint only reads back what it recorded.
func handleGetPolicyDecision(reader policyDecisionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery case id")
			return
		}

		decision, err := reader.GetLatestDecision(r.Context(), caseID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				writeError(w, http.StatusNotFound, "no policy decision found for this recovery case")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load policy decision")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(toPolicyDecisionResponse(decision))
	}
}

type policyDecisionResponse struct {
	ID                           string   `json:"id"`
	RecoveryCaseID               string   `json:"recovery_case_id"`
	RecoveryDiagnosisID          string   `json:"recovery_diagnosis_id"`
	RecoveryEconomicEvaluationID string   `json:"recovery_economic_evaluation_id"`
	Decision                     string   `json:"decision"`
	AuthorizedAction             string   `json:"authorized_action,omitempty"`
	PolicyVersion                string   `json:"policy_version"`
	ReasonCodes                  []string `json:"reason_codes"`
	Explanation                  string   `json:"explanation"`
	EvaluatedAt                  string   `json:"evaluated_at"`
	CreatedAt                    string   `json:"created_at"`
}

func toPolicyDecisionResponse(d *domain.PolicyDecision) policyDecisionResponse {
	reasonCodes := make([]string, len(d.ReasonCodes))
	for i, c := range d.ReasonCodes {
		reasonCodes[i] = string(c)
	}
	return policyDecisionResponse{
		ID:                           d.ID.String(),
		RecoveryCaseID:               d.RecoveryCaseID.String(),
		RecoveryDiagnosisID:          d.RecoveryDiagnosisID.String(),
		RecoveryEconomicEvaluationID: d.RecoveryEconomicEvaluationID.String(),
		Decision:                     string(d.Outcome),
		AuthorizedAction:             string(d.AuthorizedAction),
		PolicyVersion:                d.PolicyVersion,
		ReasonCodes:                  reasonCodes,
		Explanation:                  d.Explanation,
		EvaluatedAt:                  d.EvaluatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedAt:                    d.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
