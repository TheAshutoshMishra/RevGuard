package domain

import (
	"time"

	"github.com/google/uuid"
)

// FailureCategory is the controlled vocabulary the AI service uses to
// classify why a payment/recovery is at risk. Kept intentionally small
// and mirrored exactly by ai-service/app/models/diagnosis.py — changing
// either side without the other breaks response validation.
type FailureCategory string

const (
	FailureCategoryTransientFailure    FailureCategory = "transient_failure"
	FailureCategoryInsufficientFunds   FailureCategory = "insufficient_funds"
	FailureCategoryPaymentMethodIssue  FailureCategory = "payment_method_issue"
	FailureCategoryAuthenticationIssue FailureCategory = "authentication_issue"
	FailureCategoryMandateIssue        FailureCategory = "mandate_issue"
	FailureCategoryCustomerAbandonment FailureCategory = "customer_abandonment"
	FailureCategoryUnknown             FailureCategory = "unknown"
)

// ValidFailureCategories lists every category a RecoveryDiagnosis may hold.
var ValidFailureCategories = []FailureCategory{
	FailureCategoryTransientFailure,
	FailureCategoryInsufficientFunds,
	FailureCategoryPaymentMethodIssue,
	FailureCategoryAuthenticationIssue,
	FailureCategoryMandateIssue,
	FailureCategoryCustomerAbandonment,
	FailureCategoryUnknown,
}

func (c FailureCategory) Valid() bool {
	for _, v := range ValidFailureCategories {
		if c == v {
			return true
		}
	}
	return false
}

// RecommendedAction is the controlled vocabulary the AI service uses to
// recommend a recovery strategy. It mirrors the six identifiers in
// RecoveryActionType (lowercase snake_case here vs. UPPER_SNAKE there,
// matching each side's language conventions) but is deliberately a
// distinct Go type: a RecommendedAction is a suggestion from the AI
// service, never an authorized RecoveryAction. Conflating the two types
// would make it easy to accidentally treat a recommendation as if policy
// had already approved it — exactly the confusion "AI recommends, Policy
// decides" exists to prevent.
type RecommendedAction string

const (
	RecommendedActionRetryPayment               RecommendedAction = "retry_payment"
	RecommendedActionSendPaymentLink            RecommendedAction = "send_payment_link"
	RecommendedActionRequestPaymentMethodChange RecommendedAction = "request_payment_method_change"
	RecommendedActionSendReminder               RecommendedAction = "send_reminder"
	RecommendedActionEscalateToHuman            RecommendedAction = "escalate_to_human"
	RecommendedActionStopRecovery               RecommendedAction = "stop_recovery"
)

// ValidRecommendedActions lists every action the AI service may recommend.
var ValidRecommendedActions = []RecommendedAction{
	RecommendedActionRetryPayment,
	RecommendedActionSendPaymentLink,
	RecommendedActionRequestPaymentMethodChange,
	RecommendedActionSendReminder,
	RecommendedActionEscalateToHuman,
	RecommendedActionStopRecovery,
}

func (a RecommendedAction) Valid() bool {
	for _, v := range ValidRecommendedActions {
		if a == v {
			return true
		}
	}
	return false
}

// RecoveryDiagnosis is a single AI-generated diagnosis and recommendation
// for a RecoveryCase. It is a recommendation only: nothing in this struct
// authorizes an action, executes anything, or changes RecoveryCase state
// by itself — the orchestrator persists it and then performs the
// ANALYZING -> ANALYZED transition as a separate, explicit step. A
// RecoveryCase may accumulate more than one RecoveryDiagnosis over time
// (e.g. re-analysis after a prior AI failure); each row is immutable and
// versioned by Provider/Model/PromptVersion/GeneratedAt for
// reproducibility.
type RecoveryDiagnosis struct {
	ID             uuid.UUID
	RecoveryCaseID uuid.UUID

	FailureCategory     FailureCategory
	DiagnosisReason     string
	CustomerContext     string
	RecommendedStrategy string

	RecommendedAction    RecommendedAction
	RecommendationReason string
	Confidence           float64

	RiskFlags   []string
	Explanation string

	// Versioning metadata, recorded so a stored recommendation is
	// reproducible.
	Provider      string
	Model         string
	PromptVersion string
	GeneratedAt   time.Time

	CreatedAt time.Time
}
