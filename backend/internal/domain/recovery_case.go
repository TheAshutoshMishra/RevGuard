package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecoveryCaseStatus is the lifecycle status of a RecoveryCase.
//
// This set establishes the full vocabulary for the future recovery state
// machine (diagnosis -> policy decision -> execution -> verification). The
// state machine itself is not implemented in this milestone; only the
// domain representation and database constraints are.
type RecoveryCaseStatus string

const (
	RecoveryCaseStatusDetected    RecoveryCaseStatus = "DETECTED"
	RecoveryCaseStatusAnalyzing   RecoveryCaseStatus = "ANALYZING"
	RecoveryCaseStatusAnalyzed    RecoveryCaseStatus = "ANALYZED"
	RecoveryCaseStatusPolicyCheck RecoveryCaseStatus = "POLICY_CHECK"
	RecoveryCaseStatusAllow       RecoveryCaseStatus = "ALLOW"
	RecoveryCaseStatusBlock       RecoveryCaseStatus = "BLOCK"
	RecoveryCaseStatusEscalate    RecoveryCaseStatus = "ESCALATE"
	RecoveryCaseStatusExecuting   RecoveryCaseStatus = "EXECUTING"
	RecoveryCaseStatusVerifying   RecoveryCaseStatus = "VERIFYING"
	RecoveryCaseStatusSuccess     RecoveryCaseStatus = "SUCCESS"
	RecoveryCaseStatusFailed      RecoveryCaseStatus = "FAILED"
	RecoveryCaseStatusUnknown     RecoveryCaseStatus = "UNKNOWN"
	RecoveryCaseStatusClosed      RecoveryCaseStatus = "CLOSED"
)

// ValidRecoveryCaseStatuses lists every status a RecoveryCase may hold.
var ValidRecoveryCaseStatuses = []RecoveryCaseStatus{
	RecoveryCaseStatusDetected,
	RecoveryCaseStatusAnalyzing,
	RecoveryCaseStatusAnalyzed,
	RecoveryCaseStatusPolicyCheck,
	RecoveryCaseStatusAllow,
	RecoveryCaseStatusBlock,
	RecoveryCaseStatusEscalate,
	RecoveryCaseStatusExecuting,
	RecoveryCaseStatusVerifying,
	RecoveryCaseStatusSuccess,
	RecoveryCaseStatusFailed,
	RecoveryCaseStatusUnknown,
	RecoveryCaseStatusClosed,
}

func (s RecoveryCaseStatus) Valid() bool {
	for _, v := range ValidRecoveryCaseStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// RecoveryCase represents a revenue-at-risk case managed by RevGuard for a
// single payment belonging to a merchant/customer.
type RecoveryCase struct {
	ID            uuid.UUID
	MerchantID    uuid.UUID
	CustomerID    uuid.UUID
	PaymentID     uuid.UUID
	Status        RecoveryCaseStatus
	RevenueAtRisk Money
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ClosedAt      *time.Time
}
