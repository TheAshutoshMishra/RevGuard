package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// RazorpayProvider is a minimal, honestly-scoped Razorpay Test Mode
// adapter. Read this comment before changing or extending it.
//
// WHAT "RETRY" MEANS HERE, AND WHY:
//
// Razorpay's public API has no server-to-server "force retry this failed
// card payment" operation, and — independent of Razorpay specifically —
// Indian payment regulation (RBI-mandated additional factor
// authentication) requires the customer to re-authenticate for each card
// charge attempt. A backend cannot silently re-charge a card without the
// customer present. So "retry_payment" cannot be mapped to a literal
// "do it again" API call without inventing something Razorpay doesn't
// expose.
//
// The closest safe, real, well-documented Razorpay operation for
// initiating a retry is creating a Payment Link (POST /v1/payment_links):
// a hosted checkout page the customer can open to complete payment again.
// That is what RetryPayment calls here. A "Succeeded" result from this
// provider means "a retry mechanism was successfully created and handed
// to the customer" — it does NOT mean the payment itself succeeded.
// Final payment truth is only ever established by Milestone 7's webhook
// reconciliation, exactly like every other provider in this codebase
// (see docs/architecture/execution-engine.md).
//
// VERIFICATION STATUS: this adapter has NOT been exercised against a
// real Razorpay account in this environment — there are no Razorpay
// credentials configured, and this sandbox's outbound network access to
// arbitrary HTTPS hosts is unverified (see CLAUDE.md's Docker-limitation
// notes for the same sandbox's general network constraints). Do not
// present this provider as tested against live Razorpay Test Mode; only
// FakeProvider has been exercised by this codebase's test suite.
//
// The request/response shape below (amount in minor units, currency,
// reference_id, description; response id/short_url/status) reflects the
// Payment Links API's well-established, long-stable public shape. If
// Razorpay's actual current API differs, this adapter needs updating
// before real use — it has not been re-verified against live
// documentation in this session, per this sandbox's lack of network
// access to Razorpay's docs.
type RazorpayProvider struct {
	keyID      string
	keySecret  string
	baseURL    string
	httpClient *http.Client
}

// NewRazorpayProvider builds a RazorpayProvider. Credentials are read by
// the caller from environment variables (RAZORPAY_KEY_ID,
// RAZORPAY_KEY_SECRET) and passed in explicitly — never hardcoded, never
// logged. Returns an error if either credential is empty, so a
// misconfigured deployment fails fast rather than silently no-op'ing.
func NewRazorpayProvider(keyID, keySecret, baseURL string, httpClient *http.Client) (*RazorpayProvider, error) {
	if keyID == "" || keySecret == "" {
		return nil, errors.New("service: RazorpayProvider requires both a key ID and key secret")
	}
	if baseURL == "" {
		baseURL = "https://api.razorpay.com/v1"
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &RazorpayProvider{keyID: keyID, keySecret: keySecret, baseURL: baseURL, httpClient: httpClient}, nil
}

func (p *RazorpayProvider) Name() string { return "razorpay" }

type razorpayPaymentLinkRequest struct {
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	ReferenceID string `json:"reference_id"`
}

type razorpayPaymentLinkResponse struct {
	ID       string `json:"id"`
	ShortURL string `json:"short_url"`
	Status   string `json:"status"`
	Error    *struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	} `json:"error"`
}

func (p *RazorpayProvider) RetryPayment(ctx context.Context, request RetryPaymentRequest) (RetryPaymentResult, error) {
	result, err := p.createPaymentLink(ctx, request.AmountMinorUnits, request.Currency, request.IdempotencyKey,
		"RevGuard recovery retry for payment "+request.ExternalPaymentID)
	if err != nil {
		return RetryPaymentResult{}, err
	}
	return RetryPaymentResult{
		Succeeded: result.succeeded, ProviderReference: result.providerReference,
		ErrorCode: result.errorCode, ErrorMessage: result.errorMessage,
	}, nil
}

// SendPaymentLink (Milestone 10) calls the identical Razorpay Payment
// Links operation as RetryPayment — Razorpay's API surface offers no
// different mechanism for "proactively send a payment link" versus
// "retry via a payment link," so the only real difference is the
// description text recorded on the link and the domain.RecoveryActionType
// ExecutionEngine attaches to the resulting RecoveryAction. See
// RetryPayment's doc comment above ("WHAT 'RETRY' MEANS HERE") for the
// shared rationale and verification status, which applies identically
// here.
func (p *RazorpayProvider) SendPaymentLink(ctx context.Context, request SendPaymentLinkRequest) (SendPaymentLinkResult, error) {
	result, err := p.createPaymentLink(ctx, request.AmountMinorUnits, request.Currency, request.IdempotencyKey,
		"RevGuard payment link for payment "+request.ExternalPaymentID)
	if err != nil {
		return SendPaymentLinkResult{}, err
	}
	return SendPaymentLinkResult{
		Succeeded: result.succeeded, ProviderReference: result.providerReference,
		ErrorCode: result.errorCode, ErrorMessage: result.errorMessage,
	}, nil
}

// razorpayLinkResult is the shared, provider-shape-agnostic outcome of
// createPaymentLink; RetryPayment and SendPaymentLink each translate it
// into their own public result type.
type razorpayLinkResult struct {
	succeeded         bool
	providerReference string
	errorCode         string
	errorMessage      string
}

// createPaymentLink is the one place that actually calls Razorpay's
// POST /v1/payment_links — both RetryPayment and SendPaymentLink use it,
// so the HTTP/error-classification logic is never duplicated between the
// two actions that happen to share a gateway operation.
func (p *RazorpayProvider) createPaymentLink(ctx context.Context, amountMinorUnits int64, currency, idempotencyKey, description string) (razorpayLinkResult, error) {
	body, err := json.Marshal(razorpayPaymentLinkRequest{
		Amount:      amountMinorUnits,
		Currency:    currency,
		Description: description,
		ReferenceID: idempotencyKey,
	})
	if err != nil {
		return razorpayLinkResult{}, fmt.Errorf("service: marshal razorpay request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/payment_links", bytes.NewReader(body))
	if err != nil {
		return razorpayLinkResult{}, fmt.Errorf("service: build razorpay request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// HTTP Basic Auth per Razorpay's standard convention — credentials go
	// in the Authorization header, never the URL or body, so they can
	// never leak into logs that record request URLs.
	req.SetBasicAuth(p.keyID, p.keySecret)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Any error here happened before a response was received
		// (timeout, DNS, connection refused, TLS) — ambiguous by
		// definition. Never wrap the raw error string if it might embed
		// request details beyond the URL; http.Client errors are safe
		// here since credentials are header-based, not URL-based.
		return razorpayLinkResult{}, fmt.Errorf("service: razorpay request failed: %w", err)
	}
	defer resp.Body.Close()

	const maxBody = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return razorpayLinkResult{}, fmt.Errorf("service: read razorpay response: %w", err)
	}

	var parsed razorpayPaymentLinkResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// A response was received but isn't the shape we expect —
		// ambiguous: we can't tell if the operation happened.
		return razorpayLinkResult{}, fmt.Errorf("service: razorpay response was not the expected shape: %w", err)
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return razorpayLinkResult{succeeded: true, providerReference: parsed.ID}, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		// A 4xx means Razorpay received and definitively rejected the
		// request — a clear, actionable answer, not ambiguous.
		errorCode := "RAZORPAY_REJECTED"
		errorMessage := "razorpay: request rejected"
		if parsed.Error != nil {
			if parsed.Error.Code != "" {
				errorCode = parsed.Error.Code
			}
			errorMessage = "razorpay: " + parsed.Error.Description
		}
		return razorpayLinkResult{succeeded: false, errorCode: errorCode, errorMessage: errorMessage}, nil
	default:
		// 5xx or anything else unexpected: the server may or may not
		// have processed the request before erroring — ambiguous.
		return razorpayLinkResult{}, fmt.Errorf("service: razorpay returned HTTP %d", resp.StatusCode)
	}
}
