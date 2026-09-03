package domain

import (
	"time"

	"github.com/google/uuid"
)

// PaymentStatus is the lifecycle status of a Payment.
type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "PENDING"
	PaymentStatusSucceeded PaymentStatus = "SUCCEEDED"
	PaymentStatusFailed    PaymentStatus = "FAILED"
	PaymentStatusRefunded  PaymentStatus = "REFUNDED"
	PaymentStatusCancelled PaymentStatus = "CANCELLED"
)

// ValidPaymentStatuses lists every status a Payment may hold. Kept in sync
// with the payments_status_check constraint in the database migration.
var ValidPaymentStatuses = []PaymentStatus{
	PaymentStatusPending,
	PaymentStatusSucceeded,
	PaymentStatusFailed,
	PaymentStatusRefunded,
	PaymentStatusCancelled,
}

func (s PaymentStatus) Valid() bool {
	for _, v := range ValidPaymentStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// Payment represents a financial payment/revenue event owned by a merchant
// for one of its customers.
//
// Amount is stored as integer minor units via Money — never as a
// float/double. See Money for the rationale.
type Payment struct {
	ID                uuid.UUID
	MerchantID        uuid.UUID
	CustomerID        uuid.UUID
	ExternalPaymentID string
	Amount            Money
	Status            PaymentStatus
	PaymentMethod     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
