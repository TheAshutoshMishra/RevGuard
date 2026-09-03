package service

import "context"

// RetryPaymentRequest is everything a PaymentProvider needs to attempt a
// payment retry. It carries no card data, CVV, or authentication
// credentials — domain.Payment doesn't model those fields, so there is
// nothing to leak here (the same guarantee RecoveryContextBuilder relies
// on for the AI service boundary — see docs/architecture/ai-diagnosis.md).
type RetryPaymentRequest struct {
	// IdempotencyKey is the same key ExecutionEngine persists on the
	// RecoveryAction row (see domain.RecoveryAction.IdempotencyKey). A
	// real provider that supports request-level idempotency (e.g. an
	// "Idempotency-Key" header) should be given this value, so a
	// duplicate call from RevGuard's own retry logic is also
	// deduplicated by the provider itself wherever that's possible.
	IdempotencyKey string
	// ExternalPaymentID is domain.Payment.ExternalPaymentID — the
	// provider's own identifier for the original payment being retried.
	ExternalPaymentID string
	AmountMinorUnits  int64
	Currency          string
}

// RetryPaymentResult is a PaymentProvider's DEFINITIVE outcome for a
// retry attempt. It is only meaningful when RetryPayment returns a nil
// error — see PaymentProvider's doc comment for why ambiguous outcomes
// (timeout, transport error) are represented as a Go error instead of a
// field on this struct.
type RetryPaymentResult struct {
	// Succeeded is true for a definitive success, false for a definitive
	// failure (e.g. the provider explicitly declined/rejected the
	// retry). Only meaningful when the accompanying error is nil.
	Succeeded bool
	// ProviderReference is the provider's own identifier for whatever it
	// created/observed on success (e.g. a payment or link ID). Empty on
	// failure. Never a raw provider response — see
	// docs/architecture/execution-engine.md.
	ProviderReference string
	// ErrorCode is a short, stable, sanitized code for a definitive
	// failure (e.g. "CARD_DECLINED"). Empty on success.
	ErrorCode string
	// ErrorMessage is a short, sanitized, human-readable description of
	// a definitive failure. Never includes secrets, credentials, or raw
	// provider response bodies.
	ErrorMessage string
}

// PaymentProvider is RevGuard's abstraction over an external payment
// gateway. The Execution Engine (Milestone 6) depends on this interface
// only — it has no HTTP/transport details of its own, mirroring how
// AIClient (Milestone 3) is the only thing that knows how to reach the
// Python service.
//
// RetryPayment returns (RetryPaymentResult, nil) for any DEFINITIVE
// outcome the provider reports — success or failure — and a non-nil
// error for anything AMBIGUOUS: timeout, transport/network failure, or
// any other condition where RevGuard cannot be sure whether the
// provider-side operation happened. This mirrors AIClient.Diagnose's
// error-vs-result split (Milestone 3) and is what lets ExecutionEngine's
// classification logic stay simple: err != nil is always "treat as
// UNKNOWN, do not blindly retry" — never "treat as failure."
//
// Implementations MUST NOT include card numbers, CVV, credentials, or raw
// upstream response bodies in any returned value or error message.
type PaymentProvider interface {
	// Name identifies this provider implementation (e.g. "fake" or
	// "razorpay") for persisted/audit metadata. It must never be
	// ambiguous about whether a given execution used a real gateway.
	Name() string
	RetryPayment(ctx context.Context, request RetryPaymentRequest) (RetryPaymentResult, error)
}
