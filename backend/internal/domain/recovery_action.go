package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecoveryActionType identifies the kind of action RevGuard decided to
// attempt for a RecoveryCase. Executing these actions is not implemented
// in this milestone.
type RecoveryActionType string

const (
	RecoveryActionTypeRetryPayment               RecoveryActionType = "RETRY_PAYMENT"
	RecoveryActionTypeSendPaymentLink            RecoveryActionType = "SEND_PAYMENT_LINK"
	RecoveryActionTypeRequestPaymentMethodChange RecoveryActionType = "REQUEST_PAYMENT_METHOD_CHANGE"
	RecoveryActionTypeSendReminder               RecoveryActionType = "SEND_REMINDER"
	RecoveryActionTypeEscalateToHuman            RecoveryActionType = "ESCALATE_TO_HUMAN"
	RecoveryActionTypeStopRecovery               RecoveryActionType = "STOP_RECOVERY"
)

// ValidRecoveryActionTypes lists every action type a RecoveryAction may have.
var ValidRecoveryActionTypes = []RecoveryActionType{
	RecoveryActionTypeRetryPayment,
	RecoveryActionTypeSendPaymentLink,
	RecoveryActionTypeRequestPaymentMethodChange,
	RecoveryActionTypeSendReminder,
	RecoveryActionTypeEscalateToHuman,
	RecoveryActionTypeStopRecovery,
}

func (t RecoveryActionType) Valid() bool {
	for _, v := range ValidRecoveryActionTypes {
		if t == v {
			return true
		}
	}
	return false
}

// RecoveryActionStatus is the lifecycle status of a RecoveryAction.
type RecoveryActionStatus string

const (
	RecoveryActionStatusPending   RecoveryActionStatus = "PENDING"
	RecoveryActionStatusExecuting RecoveryActionStatus = "EXECUTING"
	RecoveryActionStatusSucceeded RecoveryActionStatus = "SUCCEEDED"
	RecoveryActionStatusFailed    RecoveryActionStatus = "FAILED"
	RecoveryActionStatusSkipped   RecoveryActionStatus = "SKIPPED"
	// RecoveryActionStatusUnknown means execution was attempted but its
	// outcome could not be definitively determined (provider timeout,
	// transport error, or an interrupted/orphaned execution attempt
	// discovered on retry — Milestone 6). It is never fabricated into
	// SUCCEEDED or FAILED; only Milestone 7's webhook/reconciliation can
	// resolve it.
	RecoveryActionStatusUnknown RecoveryActionStatus = "UNKNOWN"
)

// ValidRecoveryActionStatuses lists every status a RecoveryAction may hold.
var ValidRecoveryActionStatuses = []RecoveryActionStatus{
	RecoveryActionStatusPending,
	RecoveryActionStatusExecuting,
	RecoveryActionStatusSucceeded,
	RecoveryActionStatusFailed,
	RecoveryActionStatusSkipped,
	RecoveryActionStatusUnknown,
}

func (s RecoveryActionStatus) Valid() bool {
	for _, v := range ValidRecoveryActionStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// RecoveryAction represents an actual execution attempt RevGuard made for
// a RecoveryCase (Milestone 6) — not an AI recommendation
// (domain.RecommendedAction) and not a policy authorization
// (domain.PolicyDecision). IdempotencyKey ensures a given logical
// execution (one per PolicyDecision — see
// service.ExecutionEngine.Evaluate) is only ever executed once against
// external infrastructure, even under retries or concurrent callers.
//
// RequestedAt is when this action was created (the start of the
// execution attempt); ExecutedAt is when the provider call's outcome was
// durably recorded (nil while Status is still EXECUTING).
type RecoveryAction struct {
	ID             uuid.UUID
	RecoveryCaseID uuid.UUID
	ActionType     RecoveryActionType
	Status         RecoveryActionStatus
	AttemptNumber  int
	IdempotencyKey string
	RequestedAt    time.Time
	ExecutedAt     *time.Time
	CreatedAt      time.Time

	// Provider identifies which PaymentProvider implementation performed
	// (or attempted) this execution — e.g. "fake" or "razorpay". Recorded
	// at creation, before the provider is even called, so it's always
	// present regardless of outcome.
	Provider string
	// ProviderReference is the provider's own identifier for whatever it
	// created/observed (e.g. a payment/link ID), set only on a
	// definitive success. Never a raw provider response — see
	// docs/architecture/execution-engine.md for what is and isn't
	// persisted here.
	ProviderReference string
	// ErrorCode is a short, stable, sanitized code describing why
	// execution failed or was ambiguous (e.g. "CARD_DECLINED",
	// "PROVIDER_RESPONSE_AMBIGUOUS"). Never a raw provider error message
	// that could contain sensitive detail.
	ErrorCode string
	// ExecutionMetadata is sanitized, structured JSON describing the
	// execution attempt (outcome classification, timing) — never card
	// numbers, CVV, credentials, or raw provider HTTP responses.
	ExecutionMetadata []byte
}
