package domain

import (
	"time"

	"github.com/google/uuid"
)

// PaymentAttemptStatus is the lifecycle status of a single PaymentAttempt.
type PaymentAttemptStatus string

const (
	PaymentAttemptStatusPending   PaymentAttemptStatus = "PENDING"
	PaymentAttemptStatusSucceeded PaymentAttemptStatus = "SUCCEEDED"
	PaymentAttemptStatusFailed    PaymentAttemptStatus = "FAILED"
)

// ValidPaymentAttemptStatuses lists every status a PaymentAttempt may hold.
var ValidPaymentAttemptStatuses = []PaymentAttemptStatus{
	PaymentAttemptStatusPending,
	PaymentAttemptStatusSucceeded,
	PaymentAttemptStatusFailed,
}

func (s PaymentAttemptStatus) Valid() bool {
	for _, v := range ValidPaymentAttemptStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// PaymentAttempt represents a single attempt to process a Payment. A
// Payment is distinct from its PaymentAttempts: one payment can have many
// attempts (e.g. retries after a decline).
type PaymentAttempt struct {
	ID            uuid.UUID
	PaymentID     uuid.UUID
	AttemptNumber int
	Status        PaymentAttemptStatus
	FailureCode   string
	FailureReason string
	StartedAt     time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
}
