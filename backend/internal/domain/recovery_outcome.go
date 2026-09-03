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

// RecoveryOutcomeSource identifies what kind of trusted provider evidence
// established a RecoveryOutcome (Milestone 7). A RecoveryOutcome is only
// ever created from one of these two authoritative sources — never from a
// client request, an AI recommendation, a policy decision, or a payment
// provider's mere execution-request-accepted response.
type RecoveryOutcomeSource string

const (
	// RecoveryOutcomeSourceWebhook means an inbound, signature-verified
	// provider webhook established this outcome.
	RecoveryOutcomeSourceWebhook RecoveryOutcomeSource = "WEBHOOK"
	// RecoveryOutcomeSourceReconciliation means an explicit
	// PaymentReconciler lookup against the provider's authoritative
	// state established this outcome.
	RecoveryOutcomeSourceReconciliation RecoveryOutcomeSource = "RECONCILIATION"
)

// ValidRecoveryOutcomeSources lists every source a RecoveryOutcome may have.
var ValidRecoveryOutcomeSources = []RecoveryOutcomeSource{
	RecoveryOutcomeSourceWebhook,
	RecoveryOutcomeSourceReconciliation,
}

func (s RecoveryOutcomeSource) Valid() bool {
	for _, v := range ValidRecoveryOutcomeSources {
		if s == v {
			return true
		}
	}
	return false
}

// RecoveryOutcome represents the financial result of a RecoveryAction —
// the trusted answer to "did this actually recover revenue?" It is
// intentionally a separate record from the action itself: an action can be
// executed once (Milestone 6, "did the execution request succeed?"), but
// its financial outcome only becomes known later via webhook/reconciliation
// (Milestone 7, "did the money actually move?"). A successful provider API
// response at execution time is never sufficient evidence on its own —
// see docs/architecture/webhooks-reconciliation.md.
//
// Rows are immutable once created, and — per migration 000017's
// UNIQUE(recovery_action_id) — at most one RecoveryOutcome ever exists per
// RecoveryAction in this milestone's scope: the guarded
// RecoveryCase.Status transition (VERIFYING -> SUCCESS/FAILED, which can
// only ever succeed once) is what makes this true under concurrency, and
// the UNIQUE constraint is the defense-in-depth database-level backstop.
type RecoveryOutcome struct {
	ID                uuid.UUID
	RecoveryCaseID    uuid.UUID
	RecoveryActionID  uuid.UUID
	Status            RecoveryOutcomeStatus
	RecoveredAmount   Money
	ExternalReference string
	ObservedAt        time.Time
	CreatedAt         time.Time

	// Provider identifies which PaymentProvider/reconciliation source
	// reported this outcome (e.g. "fake" or "razorpay") — always present.
	Provider string
	// Source records whether a webhook or an explicit reconciliation call
	// established this outcome.
	Source RecoveryOutcomeSource
	// ProviderWebhookEventID references the ProviderWebhookEvent that
	// produced this outcome, when Source is WEBHOOK. Nil for
	// reconciliation-sourced outcomes, which have no corresponding
	// inbound webhook row.
	ProviderWebhookEventID *uuid.UUID
	// Metadata is sanitized, structured JSON with normalized details
	// useful for audit (e.g. the provider event type) — never a raw
	// provider response body, never credentials.
	Metadata []byte
}
