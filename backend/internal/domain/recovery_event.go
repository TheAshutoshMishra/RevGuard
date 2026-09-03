package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecoveryEventType identifies the kind of domain event that occurred.
// Producing/consuming these via Redpanda is not implemented in this
// milestone; this establishes the domain vocabulary and storage only.
type RecoveryEventType string

const (
	RecoveryEventTypePaymentFailed      RecoveryEventType = "payment.failed"
	RecoveryEventTypePaymentSucceeded   RecoveryEventType = "payment.succeeded"
	RecoveryEventTypeCheckoutAbandoned  RecoveryEventType = "checkout.abandoned"
	RecoveryEventTypeSubscriptionFailed RecoveryEventType = "subscription.failed"
	RecoveryEventTypeMandateFailed      RecoveryEventType = "mandate.failed"
	RecoveryEventTypeInvoiceOverdue     RecoveryEventType = "invoice.overdue"
	RecoveryEventTypeRecoveryCreated    RecoveryEventType = "recovery.created"
	RecoveryEventTypeRecoveryAnalyzed   RecoveryEventType = "recovery.analyzed"
	RecoveryEventTypeRecoveryAuthorized RecoveryEventType = "recovery.authorized"
	RecoveryEventTypeRecoveryBlocked    RecoveryEventType = "recovery.blocked"
	RecoveryEventTypeRecoveryEscalated  RecoveryEventType = "recovery.escalated"
	RecoveryEventTypeRecoveryAttempted  RecoveryEventType = "recovery.attempted"
	RecoveryEventTypeRecoverySucceeded  RecoveryEventType = "recovery.succeeded"
	RecoveryEventTypeRecoveryFailed     RecoveryEventType = "recovery.failed"
	RecoveryEventTypeRecoveryUnknown    RecoveryEventType = "recovery.unknown"
)

// ValidRecoveryEventTypes lists every event type a RecoveryEvent may have.
var ValidRecoveryEventTypes = []RecoveryEventType{
	RecoveryEventTypePaymentFailed,
	RecoveryEventTypePaymentSucceeded,
	RecoveryEventTypeCheckoutAbandoned,
	RecoveryEventTypeSubscriptionFailed,
	RecoveryEventTypeMandateFailed,
	RecoveryEventTypeInvoiceOverdue,
	RecoveryEventTypeRecoveryCreated,
	RecoveryEventTypeRecoveryAnalyzed,
	RecoveryEventTypeRecoveryAuthorized,
	RecoveryEventTypeRecoveryBlocked,
	RecoveryEventTypeRecoveryEscalated,
	RecoveryEventTypeRecoveryAttempted,
	RecoveryEventTypeRecoverySucceeded,
	RecoveryEventTypeRecoveryFailed,
	RecoveryEventTypeRecoveryUnknown,
}

func (t RecoveryEventType) Valid() bool {
	for _, v := range ValidRecoveryEventTypes {
		if t == v {
			return true
		}
	}
	return false
}

// RecoveryEvent represents a domain event associated with recovery, keyed
// by a stable EventID so later ingestion (e.g. from Redpanda or webhooks)
// can be processed idempotently.
type RecoveryEvent struct {
	ID            uuid.UUID
	EventID       string
	EventType     RecoveryEventType
	AggregateType string
	AggregateID   uuid.UUID
	MerchantID    uuid.UUID
	Payload       []byte // raw JSON
	OccurredAt    time.Time
	CreatedAt     time.Time
	// RecoveryCaseID is set once this event has been correlated to a
	// RecoveryCase (Milestone 2). Nil for events that never qualify for
	// case creation (e.g. payment.succeeded).
	RecoveryCaseID *uuid.UUID
}
