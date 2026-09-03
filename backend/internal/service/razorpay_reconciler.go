package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"revguard/backend/internal/domain"
)

// RazorpayReconciler answers "what does Razorpay's own state say
// happened?" by fetching the Payment Link resource Razorpay's documented
// "Fetch a Payment Link" API exposes (GET /v1/payment_links/{id}) — the
// natural counterpart to RazorpayProvider's Payment Link creation
// (Milestone 6). Read-only: this adapter never creates, updates, or
// cancels anything.
//
// VERIFICATION STATUS: NOT VERIFIED against a real Razorpay account or
// current live documentation in this session — no credentials are
// configured in this sandbox and there is no confirmed outbound network
// access to Razorpay's API. Written from Razorpay's long-documented,
// stable Payment Link entity shape (`status`, `amount_paid`, `currency`,
// `payments[]`). Same honesty caveat as RazorpayProvider and
// RazorpayWebhookVerifier — see
// docs/architecture/webhooks-reconciliation.md.
//
// SCOPE: "partially_paid" is treated as PENDING (inconclusive), not a
// definitive terminal outcome — domain.RecoveryOutcomeStatus has no
// partial-success vocabulary, and guessing a partial capture into either
// SUCCESS or FAILED would violate "never guess." Extending outcome
// vocabulary to represent partial recovery is future work, not this
// milestone's scope.
type RazorpayReconciler struct {
	keyID      string
	keySecret  string
	baseURL    string
	httpClient *http.Client
}

func NewRazorpayReconciler(keyID, keySecret, baseURL string, httpClient *http.Client) (*RazorpayReconciler, error) {
	if keyID == "" || keySecret == "" {
		return nil, fmt.Errorf("service: RazorpayReconciler requires both a key ID and key secret")
	}
	if baseURL == "" {
		baseURL = "https://api.razorpay.com/v1"
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &RazorpayReconciler{keyID: keyID, keySecret: keySecret, baseURL: baseURL, httpClient: httpClient}, nil
}

type razorpayPaymentLinkFetchResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Currency   string `json:"currency"`
	AmountPaid int64  `json:"amount_paid"`
	Payments   []struct {
		PaymentID string `json:"payment_id"`
		Status    string `json:"status"`
	} `json:"payments"`
}

func (r *RazorpayReconciler) Reconcile(ctx context.Context, request ReconciliationRequest) (ReconciliationResult, error) {
	if request.Provider != "razorpay" {
		return ReconciliationResult{}, fmt.Errorf("%w: %s", ErrUnknownReconciliationProvider, request.Provider)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/payment_links/"+request.ProviderReference, nil)
	if err != nil {
		return ReconciliationResult{}, fmt.Errorf("service: build razorpay reconciliation request: %w", err)
	}
	req.SetBasicAuth(r.keyID, r.keySecret)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		// Before any response was received — ambiguous.
		return ReconciliationResult{}, fmt.Errorf("service: razorpay reconciliation request failed: %w", err)
	}
	defer resp.Body.Close()

	const maxBody = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return ReconciliationResult{}, fmt.Errorf("service: read razorpay reconciliation response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return ReconciliationResult{}, fmt.Errorf("%w: %s", ErrReconciliationReferenceNotFound, request.ProviderReference)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Any other non-2xx (including 5xx) — the provider may or may not
		// have a real answer; ambiguous.
		return ReconciliationResult{}, fmt.Errorf("service: razorpay reconciliation returned HTTP %d", resp.StatusCode)
	}

	var parsed razorpayPaymentLinkFetchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ReconciliationResult{}, fmt.Errorf("service: razorpay reconciliation response was not the expected shape: %w", err)
	}

	switch parsed.Status {
	case "paid":
		paymentRef := ""
		for _, p := range parsed.Payments {
			if p.Status == "captured" {
				paymentRef = p.PaymentID
				break
			}
		}
		return ReconciliationResult{
			Status:                   domain.ProviderEventStatusCaptured,
			AmountMinorUnits:         parsed.AmountPaid,
			Currency:                 parsed.Currency,
			ProviderPaymentReference: paymentRef,
			OccurredAt:               time.Now().UTC(),
		}, nil
	case "cancelled", "expired":
		return ReconciliationResult{Status: domain.ProviderEventStatusFailed, OccurredAt: time.Now().UTC()}, nil
	default:
		// "created", "partially_paid", or anything unrecognized — not
		// definitive, never guessed.
		return ReconciliationResult{Status: domain.ProviderEventStatusPending}, nil
	}
}
