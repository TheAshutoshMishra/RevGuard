package domain

import (
	"time"

	"github.com/google/uuid"
)

// ProviderEventStatus is the normalized, provider-agnostic status a
// ProviderWebhookEvent (or a PaymentReconciler lookup) can report. It
// deliberately does not include SUCCESS/FAILED (domain.RecoveryOutcomeStatus)
// — a webhook/reconciliation observation is not itself a financial
// outcome, it is evidence that may or may not be strong enough to
// establish one (see the CAPTURED/FAILED -> SUCCESS/FAILED mapping in
// backend/internal/service/financial_outcome.go).
type ProviderEventStatus string

const (
	// ProviderEventStatusCaptured means the provider reports the payment
	// was definitively captured/paid — strong evidence for a SUCCESS
	// RecoveryOutcome.
	ProviderEventStatusCaptured ProviderEventStatus = "CAPTURED"
	// ProviderEventStatusFailed means the provider reports the payment
	// definitively did not and will not succeed (e.g. a payment link was
	// cancelled or expired unpaid) — strong evidence for a FAILED
	// RecoveryOutcome.
	ProviderEventStatusFailed ProviderEventStatus = "FAILED"
	// ProviderEventStatusPending means the event does not provide enough
	// evidence to establish a definitive financial outcome (e.g. a
	// payment link is still open, or a partially-paid state). No
	// RecoveryOutcome is ever created for a PENDING observation — the
	// case remains VERIFYING and nothing is guessed.
	ProviderEventStatusPending ProviderEventStatus = "PENDING"
)

// ValidProviderEventStatuses lists every status a ProviderWebhookEvent may hold.
var ValidProviderEventStatuses = []ProviderEventStatus{
	ProviderEventStatusCaptured,
	ProviderEventStatusFailed,
	ProviderEventStatusPending,
}

func (s ProviderEventStatus) Valid() bool {
	for _, v := range ValidProviderEventStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// ProviderWebhookEvent is the durable, normalized record of a single
// inbound provider webhook notification (Milestone 7). It is both:
//
//  1. the append-only ingestion ledger — every authenticated,
//     successfully-parsed webhook delivery gets exactly one row, even if
//     it could not be correlated to a RecoveryAction; and
//  2. the sole idempotency authority for webhook delivery — the
//     UNIQUE(provider, provider_event_id) constraint (migration 000016)
//     is what makes redelivery (Razorpay's webhooks are at-least-once)
//     safe.
//
// Only normalized fields are stored — never the raw webhook request body,
// which may contain more provider detail than reconciliation/audit
// requires and is unnecessary to retain once signature verification has
// already happened at the HTTP boundary.
type ProviderWebhookEvent struct {
	ID uuid.UUID

	Provider          string // e.g. "razorpay"
	ProviderEventID   string // provider's own idempotency identifier for this notification
	EventType         string // provider's event type string, e.g. "payment_link.paid"
	ProviderReference string // the provider-side object this event concerns (e.g. a payment link ID)
	Status            ProviderEventStatus
	AmountMinorUnits  int64
	Currency          Currency
	OccurredAt        time.Time // provider-reported event time

	// RecoveryActionID is set once this event is correlated to a
	// RecoveryAction via ProviderReference. Nil when unmatched — an
	// unmatched event is still durably recorded (never discarded), just
	// with no downstream financial effect.
	RecoveryActionID *uuid.UUID
	Matched          bool

	Metadata   []byte // sanitized, normalized JSON — never a raw payload
	ReceivedAt time.Time
	CreatedAt  time.Time
}
