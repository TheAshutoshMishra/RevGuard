package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"revguard/backend/internal/service"
)

// eventProcessor is the subset of service.EventProcessor's API this
// handler needs. Defined at the point of use so the handler can be
// exercised with a fake in tests without a real database.
type eventProcessor interface {
	Process(ctx context.Context, in service.EventInput) (*service.ProcessResult, error)
}

// handleCreateEvent decodes and delegates a raw event to the event
// processing service. It contains no business logic: validation,
// idempotency, persistence, and recovery case orchestration all happen in
// backend/internal/service.
func handleCreateEvent(processor eventProcessor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input service.EventInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		result, err := processor.Process(r.Context(), input)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidEvent):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, service.ErrAggregateNotFound),
				errors.Is(err, service.ErrUnsupportedAggregate),
				errors.Is(err, service.ErrMerchantMismatch):
				writeError(w, http.StatusUnprocessableEntity, err.Error())
			default:
				// Never leak raw persistence/driver errors to callers.
				writeError(w, http.StatusInternalServerError, "failed to process event")
			}
			return
		}

		status := http.StatusCreated
		if result.Duplicate {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(toEventResponse(result))
	}
}

type eventResponse struct {
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	Duplicate      bool   `json:"duplicate"`
	CaseCreated    bool   `json:"case_created"`
	RecoveryCaseID string `json:"recovery_case_id,omitempty"`
	CaseStatus     string `json:"case_status,omitempty"`
}

func toEventResponse(r *service.ProcessResult) eventResponse {
	resp := eventResponse{
		EventID:     r.Event.EventID,
		EventType:   string(r.Event.EventType),
		Duplicate:   r.Duplicate,
		CaseCreated: r.CaseCreated,
	}
	if r.RecoveryCase != nil {
		resp.RecoveryCaseID = r.RecoveryCase.ID.String()
		resp.CaseStatus = string(r.RecoveryCase.Status)
	}
	return resp
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
