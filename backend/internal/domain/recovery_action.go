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
)

// ValidRecoveryActionStatuses lists every status a RecoveryAction may hold.
var ValidRecoveryActionStatuses = []RecoveryActionStatus{
	RecoveryActionStatusPending,
	RecoveryActionStatusExecuting,
	RecoveryActionStatusSucceeded,
	RecoveryActionStatusFailed,
	RecoveryActionStatusSkipped,
}

func (s RecoveryActionStatus) Valid() bool {
	for _, v := range ValidRecoveryActionStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// RecoveryAction represents an action that RevGuard decided to attempt for
// a RecoveryCase. IdempotencyKey ensures a given action is only ever
// executed once by infrastructure, even under retries.
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
}
