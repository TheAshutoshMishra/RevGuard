package service

import (
	"time"

	"revguard/backend/internal/domain"
)

// ParsedProviderEvent is the normalized, provider-agnostic representation
// a ProviderEventParser produces from a raw webhook body. WebhookProcessor
// operates only on this shape — no Razorpay-specific JSON structure leaks
// past the parser (see razorpay_webhook_parser.go, the only file that
// knows Razorpay's actual payload shape).
type ParsedProviderEvent struct {
	Provider string

	// ProviderEventID is the provider's own idempotency identifier for
	// this notification. See razorpay_webhook_parser.go's doc comment for
	// how this is derived and the honesty caveat around it.
	ProviderEventID string
	EventType       string

	// ProviderReference is the provider-side object this event concerns
	// (e.g. a payment link ID) — the value WebhookProcessor correlates
	// against RecoveryAction.ProviderReference. Empty if the payload
	// doesn't carry an object this system's execution paths could have
	// produced; such an event is not malformed, just never correlatable
	// (handled as unmatched).
	ProviderReference string

	Status           domain.ProviderEventStatus
	AmountMinorUnits int64
	Currency         domain.Currency

	OccurredAt time.Time
}

// ProviderEventParser turns a raw, already signature-verified webhook body
// into a ParsedProviderEvent. Implementations are the only place in the
// codebase that know a specific provider's JSON schema.
type ProviderEventParser interface {
	Parse(rawBody []byte, eventIDHeader string) (*ParsedProviderEvent, error)
}
