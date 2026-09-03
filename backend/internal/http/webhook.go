package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"revguard/backend/internal/service"
)

// maxWebhookBodyBytes bounds how much of an inbound webhook request body
// is ever read into memory. Razorpay payloads are small JSON documents;
// this is a defensive cap against a misbehaving or malicious sender, not
// a tuned production limit.
const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// webhookProcessor is the subset of service.WebhookProcessor's API this
// handler needs. Defined at the point of use so the handler can be
// exercised with a fake in tests without a real database.
type webhookProcessor interface {
	Process(ctx context.Context, rawBody []byte, signatureHeader, eventIDHeader string) (*service.WebhookOutcome, error)
}

// handleRazorpayWebhook is POST /v1/webhooks/razorpay.
//
// The raw request body is read and passed to WebhookProcessor.Process
// completely unmodified — signature verification must run against the
// exact bytes Razorpay signed, not a re-marshaled JSON representation of
// them. Nothing about the request (headers, body, query string) is ever
// trusted enough to change RecoveryCase state on its own: Process only
// ever acts on a signature-verified, successfully-parsed, and
// provider-reference-correlated event. See
// docs/architecture/webhooks-reconciliation.md.
//
// Every response is 2xx unless the request itself was invalid/unverifiable
// or something on our side genuinely failed — Razorpay's webhook delivery
// retries on non-2xx, and both a duplicate delivery and an unmatched event
// are expected, successfully-handled outcomes, not errors.
func handleRazorpayWebhook(processor webhookProcessor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawBody, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		if len(rawBody) > maxWebhookBodyBytes {
			writeError(w, http.StatusBadRequest, "request body too large")
			return
		}

		signatureHeader := r.Header.Get("X-Razorpay-Signature")
		eventIDHeader := r.Header.Get("X-Razorpay-Event-Id")

		outcome, err := processor.Process(r.Context(), rawBody, signatureHeader, eventIDHeader)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidWebhookSignature):
				// Deliberately generic: never confirm/deny which part of
				// the signature check failed to an unauthenticated caller.
				writeError(w, http.StatusUnauthorized, "invalid webhook signature")
			case errors.Is(err, service.ErrMalformedWebhookPayload):
				writeError(w, http.StatusBadRequest, "malformed webhook payload")
			default:
				// Never leak raw persistence errors to callers.
				writeError(w, http.StatusInternalServerError, "failed to process webhook")
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(toWebhookResponse(outcome))
	}
}

type webhookResponse struct {
	ProviderWebhookEventID  string `json:"provider_webhook_event_id"`
	Duplicate               bool   `json:"duplicate"`
	Matched                 bool   `json:"matched"`
	FinancialOutcomeApplied bool   `json:"financial_outcome_applied"`
	CaseStatus              string `json:"case_status,omitempty"`
}

func toWebhookResponse(o *service.WebhookOutcome) webhookResponse {
	resp := webhookResponse{
		ProviderWebhookEventID:  o.Event.ID.String(),
		Duplicate:               o.Duplicate,
		Matched:                 o.Event.Matched,
		FinancialOutcomeApplied: o.FinancialOutcomeApplied,
	}
	if o.Case != nil {
		resp.CaseStatus = string(o.Case.Status)
	}
	return resp
}
