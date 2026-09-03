package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"revguard/backend/internal/domain"
)

// RazorpayWebhookParser turns a raw Razorpay webhook body into a
// ParsedProviderEvent. Read this comment before changing or extending it.
//
// SCOPE: Milestone 6's RazorpayProvider implements retry_payment only via
// Payment Link creation (see docs/architecture/execution-engine.md) — so
// this parser is deliberately scoped to the Payment Link event lifecycle,
// not Razorpay's full webhook catalog (avoiding the "broad Razorpay API
// surface" scope creep this project's milestones are explicitly told to
// avoid). It normalizes exactly the events needed to answer "did the
// payment link actually get paid, or definitively not":
//
//   - "payment_link.paid"      -> ProviderEventStatusCaptured
//   - "payment_link.cancelled" -> ProviderEventStatusFailed
//   - "payment_link.expired"   -> ProviderEventStatusFailed
//   - anything else            -> ProviderEventStatusPending (inconclusive;
//     never guessed into a definitive outcome)
//
// PAYLOAD SHAPE: written from Razorpay's long-documented webhook envelope
// (`event`, `payload.payment_link.entity`, `payload.payment.entity`,
// `created_at`) and Payment Link entity fields (`id`, `amount`,
// `currency`, `status`, `amount_paid`). NOT re-verified against current
// live Razorpay documentation or a real webhook delivery in this session —
// same honesty caveat as RazorpayProvider (Milestone 6) and
// RazorpayReconciler. If Razorpay's actual current schema differs, this
// parser needs updating before real use.
//
// EVENT ID: Razorpay is documented to send a per-delivery idempotency
// identifier via the `X-Razorpay-Event-Id` request header. This parser
// uses that header when present. When absent (unverified whether every
// Razorpay account/API version sends it), it falls back to a SHA-256 hash
// of the raw request body as a deterministic idempotency key — exact
// redelivery (Razorpay resends the identical body on retry) still dedupes
// correctly under UNIQUE(provider, provider_event_id), just with a
// different (still stable) key shape. This fallback is a documented
// assumption, not a confirmed Razorpay behavior.
type RazorpayWebhookParser struct{}

func NewRazorpayWebhookParser() *RazorpayWebhookParser {
	return &RazorpayWebhookParser{}
}

type razorpayWebhookEnvelope struct {
	Event   string `json:"event"`
	Payload struct {
		PaymentLink *struct {
			Entity struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"entity"`
		} `json:"payment_link"`
		Payment *struct {
			Entity struct {
				ID       string `json:"id"`
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
				Status   string `json:"status"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
	CreatedAt int64 `json:"created_at"`
}

func (p *RazorpayWebhookParser) Parse(rawBody []byte, eventIDHeader string) (*ParsedProviderEvent, error) {
	var envelope razorpayWebhookEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedWebhookPayload, err)
	}
	if envelope.Event == "" {
		return nil, fmt.Errorf("%w: missing \"event\" field", ErrMalformedWebhookPayload)
	}

	providerReference := ""
	if envelope.Payload.PaymentLink != nil && envelope.Payload.PaymentLink.Entity.ID != "" {
		providerReference = envelope.Payload.PaymentLink.Entity.ID
	} else if envelope.Payload.Payment != nil && envelope.Payload.Payment.Entity.ID != "" {
		providerReference = envelope.Payload.Payment.Entity.ID
	}

	status := domain.ProviderEventStatusPending
	switch envelope.Event {
	case "payment_link.paid":
		status = domain.ProviderEventStatusCaptured
	case "payment_link.cancelled", "payment_link.expired":
		status = domain.ProviderEventStatusFailed
	}

	var amountMinorUnits int64
	var currency domain.Currency
	if envelope.Payload.Payment != nil {
		amountMinorUnits = envelope.Payload.Payment.Entity.Amount
		if c, err := domain.NewCurrency(envelope.Payload.Payment.Entity.Currency); err == nil {
			currency = c
		}
	}

	occurredAt := time.Now().UTC()
	if envelope.CreatedAt > 0 {
		occurredAt = time.Unix(envelope.CreatedAt, 0).UTC()
	}

	eventID := eventIDHeader
	if eventID == "" {
		sum := sha256.Sum256(rawBody)
		eventID = "bodyhash:" + hex.EncodeToString(sum[:])
	}

	return &ParsedProviderEvent{
		Provider:          "razorpay",
		ProviderEventID:   eventID,
		EventType:         envelope.Event,
		ProviderReference: providerReference,
		Status:            status,
		AmountMinorUnits:  amountMinorUnits,
		Currency:          currency,
		OccurredAt:        occurredAt,
	}, nil
}
