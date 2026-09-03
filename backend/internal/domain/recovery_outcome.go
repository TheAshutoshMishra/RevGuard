package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecoveryOutcomeStatus is the financial result status of a RecoveryAction.
type RecoveryOutcomeStatus string

const (
	RecoveryOutcomeStatusSuccess RecoveryOutcomeStatus = "SUCCESS"
	RecoveryOutcomeStatusFailed  RecoveryOutcomeStatus = "FAILED"
	RecoveryOutcomeStatusUnknown RecoveryOutcomeStatus = "UNKNOWN"
)

// ValidRecoveryOutcomeStatuses lists every status a RecoveryOutcome may hold.
var ValidRecoveryOutcomeStatuses = []RecoveryOutcomeStatus{
	RecoveryOutcomeStatusSuccess,
	RecoveryOutcomeStatusFailed,
	RecoveryOutcomeStatusUnknown,
}

func (s RecoveryOutcomeStatus) Valid() bool {
	for _, v := range ValidRecoveryOutcomeStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// RecoveryOutcome represents the financial result of a RecoveryAction. It
// is intentionally a separate record from the action itself: an action can
// be executed once, but its outcome may only become known later (or be
// revised) via webhook/reconciliation logic in a future milestone.
type RecoveryOutcome struct {
	ID                uuid.UUID
	RecoveryCaseID    uuid.UUID
	RecoveryActionID  uuid.UUID
	Status            RecoveryOutcomeStatus
	RecoveredAmount   Money
	ExternalReference string
	ObservedAt        time.Time
	CreatedAt         time.Time
}
