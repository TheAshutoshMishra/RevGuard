package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"revguard/backend/internal/service"
)

// executionExecutor is the subset of service.ExecutionEngine's API this
// handler needs. Defined at the point of use so the handler can be
// exercised with a fake in tests without a real database or provider.
type executionExecutor interface {
	Execute(ctx context.Context, recoveryCaseID, policyDecisionID uuid.UUID) (*service.ExecutionOutcome, error)
}

// handleExecuteRecoveryCase is POST /v1/recovery-cases/{id}/execute.
//
// The request body is deliberately empty: the caller supplies only the
// case ID in the URL. Go resolves the latest policy decision for that
// case server-side and passes its ID to service.ExecutionEngine.Execute,
// which independently reloads and re-validates it from PostgreSQL —
// there is no request parameter, query string, or body field through
// which a client could specify an action, a policy decision, or any
// other execution input. See docs/architecture/execution-engine.md.
func handleExecuteRecoveryCase(decisions policyDecisionReader, executor executionExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery case id")
			return
		}

		decision, err := decisions.GetLatestDecision(r.Context(), caseID)
		if err != nil {
			writeError(w, http.StatusNotFound, "no policy decision found for this recovery case")
			return
		}

		outcome, err := executor.Execute(r.Context(), caseID, decision.ID)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrPolicyDecisionNotFound),
				errors.Is(err, service.ErrPolicyDecisionCaseMismatch),
				errors.Is(err, service.ErrRecoveryCaseNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, service.ErrPolicyDecisionNotAllow),
				errors.Is(err, service.ErrMissingAuthorizedAction),
				errors.Is(err, service.ErrActionNotExecutable),
				errors.Is(err, service.ErrRecoveryCaseNotAllow):
				writeError(w, http.StatusUnprocessableEntity, err.Error())
			default:
				// Never leak raw persistence/provider errors to callers.
				writeError(w, http.StatusInternalServerError, "failed to execute recovery case")
			}
			return
		}

		status := http.StatusCreated
		if !outcome.Created {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(toExecutionResponse(outcome))
	}
}

type executionResponse struct {
	RecoveryCaseID    string `json:"recovery_case_id"`
	RecoveryActionID  string `json:"recovery_action_id"`
	AuthorizedAction  string `json:"authorized_action"`
	Provider          string `json:"provider"`
	ExecutionStatus   string `json:"execution_status"`
	CaseStatus        string `json:"case_status"`
	ProviderReference string `json:"provider_reference,omitempty"`
	Unknown           bool   `json:"unknown"`
}

func toExecutionResponse(o *service.ExecutionOutcome) executionResponse {
	return executionResponse{
		RecoveryCaseID:    o.Case.ID.String(),
		RecoveryActionID:  o.Action.ID.String(),
		AuthorizedAction:  string(o.Action.ActionType),
		Provider:          o.Action.Provider,
		ExecutionStatus:   string(o.Action.Status),
		CaseStatus:        string(o.Case.Status),
		ProviderReference: o.Action.ProviderReference,
		Unknown:           o.Action.Status == "UNKNOWN",
	}
}
