package service

import (
	"context"
	"time"

	"revguard/backend/internal/domain"
)

// ReconciliationRequest identifies what to look up on the provider side.
// It carries only identifiers already durably recorded on the
// RecoveryAction being reconciled — never anything a caller supplies.
type ReconciliationRequest struct {
	Provider          string
	ProviderReference string
}

// ReconciliationResult is a PaymentReconciler's DEFINITIVE finding. Only
// meaningful when Reconcile returns a nil error — see PaymentReconciler's
// doc comment for why an inconclusive/ambiguous lookup is represented as a
// Go error instead of a field here.
type ReconciliationResult struct {
	Status domain.ProviderEventStatus // CAPTURED, FAILED, or PENDING

	// AmountMinorUnits/Currency are the provider-confirmed captured
	// amount, meaningful only when Status is CAPTURED. Never derived from
	// the original payment amount or any RevGuard-internal estimate —
	// see docs/architecture/webhooks-reconciliation.md.
	AmountMinorUnits int64
	Currency         string

	// ProviderPaymentReference is the provider's own identifier for the
	// specific captured payment (e.g. a "pay_xxx" ID), distinct from the
	// ProviderReference that was looked up (e.g. a payment link ID) —
	// kept for audit only, never a raw provider response.
	ProviderPaymentReference string

	OccurredAt time.Time
}

// PaymentReconciler answers "what does the provider's own authoritative
// state say actually happened?" for a given provider reference. It is
// read-only by construction: no implementation of this interface may
// execute a payment, create a payment link, or otherwise cause a new
// financial side effect — reconciliation means "find out what already
// happened," never "perform the action again." See
// docs/architecture/webhooks-reconciliation.md.
//
// Reconcile returns (ReconciliationResult, nil) for any DEFINITIVE
// provider-side answer — including PENDING, which is a definitive "not
// resolved yet, don't guess" answer, not an error — and a non-nil error
// for anything AMBIGUOUS: timeout, transport failure, or the provider
// reporting no record of the reference at all. This mirrors
// PaymentProvider.RetryPayment's error-vs-result split (Milestone 6) so
// callers can treat "err != nil" uniformly as "stay UNKNOWN, do not
// guess, do not retry the payment."
type PaymentReconciler interface {
	Reconcile(ctx context.Context, request ReconciliationRequest) (ReconciliationResult, error)
}
