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

// reconciliationEngine is the subset of service.ReconciliationEngine's API
// this handler needs. Defined at the point of use so the handler can be
// exercised with a fake in tests without a real database.
type reconciliationEngine interface {
	Reconcile(ctx context.Context, recoveryCaseID uuid.UUID) (*service.ReconciliationOutcome, error)
}

// handleReconcileRecoveryCase is POST /v1/recovery-cases/{id}/reconcile.
//
// The request body is deliberately empty, the same convention Milestone
// 6's /execute endpoint established: the caller supplies only the case ID
// in the URL, and ReconciliationEngine independently reloads and
// re-validates everything else from PostgreSQL. There is no request field
// through which a client could assert an outcome, an amount, or a
// provider reference — reconciliation only ever trusts what
// PaymentReconciler reports or what RevGuard already durably recorded.
func handleReconcileRecoveryCase(reconciler reconciliationEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery case id")
			return
		}

		outcome, err := reconciler.Reconcile(r.Context(), caseID)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrRecoveryCaseNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, service.ErrRecoveryCaseNotVerifying),
				errors.Is(err, service.ErrNoRecoveryActionForCase):
				writeError(w, http.StatusUnprocessableEntity, err.Error())
			default:
				// Never leak raw persistence/provider errors to callers.
				writeError(w, http.StatusInternalServerError, "failed to reconcile recovery case")
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(toReconciliationResponse(outcome))
	}
}

type reconciliationResponse struct {
	RecoveryCaseID   string `json:"recovery_case_id"`
	RecoveryActionID string `json:"recovery_action_id"`
	CaseStatus       string `json:"case_status"`
	Applied          bool   `json:"applied"`
}

func toReconciliationResponse(o *service.ReconciliationOutcome) reconciliationResponse {
	return reconciliationResponse{
		RecoveryCaseID:   o.Case.ID.String(),
		RecoveryActionID: o.Action.ID.String(),
		CaseStatus:       string(o.Case.Status),
		Applied:          o.Applied,
	}
}
