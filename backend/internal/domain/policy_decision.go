package domain

import (
	"time"

	"github.com/google/uuid"
)

// PolicyDecisionOutcome is the Policy Engine's authorization verdict for
// a diagnosed, economically-evaluated recommendation.
//
// Its three values are deliberately the same strings as the
// corresponding RecoveryCaseStatus values (RecoveryCaseStatusAllow,
// RecoveryCaseStatusBlock, RecoveryCaseStatusEscalate) — unlike
// RecommendedAction vs. RecoveryActionType (Milestone 3/4), where the
// two vocabularies were deliberately kept distinct to prevent an AI
// recommendation from being confused with an authorized action, a
// PolicyDecisionOutcome IS, by construction, the RecoveryCaseStatus the
// case transitions into: there is no risk of conflating a suggestion
// with an authorization here, because this outcome IS the authorization.
// See service.PolicyEngine.Evaluate, which converts one directly to the
// other.
type PolicyDecisionOutcome string

const (
	PolicyDecisionOutcomeAllow    PolicyDecisionOutcome = "ALLOW"
	PolicyDecisionOutcomeBlock    PolicyDecisionOutcome = "BLOCK"
	PolicyDecisionOutcomeEscalate PolicyDecisionOutcome = "ESCALATE"
)

// ValidPolicyDecisionOutcomes lists every outcome a PolicyDecision may hold.
var ValidPolicyDecisionOutcomes = []PolicyDecisionOutcome{
	PolicyDecisionOutcomeAllow,
	PolicyDecisionOutcomeBlock,
	PolicyDecisionOutcomeEscalate,
}

func (o PolicyDecisionOutcome) Valid() bool {
	for _, v := range ValidPolicyDecisionOutcomes {
		if o == v {
			return true
		}
	}
	return false
}

// PolicyReasonCode is a stable, typed identifier for why a policy rule
// fired. A PolicyDecision may carry more than one — every rule that
// applies is recorded, not just the first/most severe one, so the
// decision is fully explainable.
type PolicyReasonCode string

const (
	PolicyReasonStopRecoveryRecommendation PolicyReasonCode = "STOP_RECOVERY_RECOMMENDATION"
	PolicyReasonLowAIConfidence            PolicyReasonCode = "LOW_AI_CONFIDENCE"
	PolicyReasonNegativeExpectedValue      PolicyReasonCode = "NEGATIVE_EXPECTED_VALUE"
	PolicyReasonAmountAboveAutoLimit       PolicyReasonCode = "AMOUNT_ABOVE_AUTO_LIMIT"
	PolicyReasonMaxAttemptsReached         PolicyReasonCode = "MAX_ATTEMPTS_REACHED"
	PolicyReasonTooManyPriorActions        PolicyReasonCode = "TOO_MANY_PRIOR_ACTIONS"
	PolicyReasonActionNotAutoAllowed       PolicyReasonCode = "ACTION_NOT_AUTO_ALLOWED"
	PolicyReasonPolicyAllowed              PolicyReasonCode = "POLICY_ALLOWED"
)

// ValidPolicyReasonCodes lists every reason code a PolicyDecision may carry.
var ValidPolicyReasonCodes = []PolicyReasonCode{
	PolicyReasonStopRecoveryRecommendation,
	PolicyReasonLowAIConfidence,
	PolicyReasonNegativeExpectedValue,
	PolicyReasonAmountAboveAutoLimit,
	PolicyReasonMaxAttemptsReached,
	PolicyReasonTooManyPriorActions,
	PolicyReasonActionNotAutoAllowed,
	PolicyReasonPolicyAllowed,
}

func (c PolicyReasonCode) Valid() bool {
	for _, v := range ValidPolicyReasonCodes {
		if c == v {
			return true
		}
	}
	return false
}

// PolicyDecision is the durable, immutable, auditable record of a single
// Policy Engine evaluation (Milestone 5).
//
// It references the exact RecoveryDiagnosis and RecoveryEconomicEvaluation
// it evaluated — never "the latest as of now" — so a decision stays
// reproducible even if the case is later re-analyzed and re-evaluated.
// Rows here are never updated after creation; a re-evaluation of the
// same (case, diagnosis, evaluation, policy version) tuple returns the
// existing row rather than creating or modifying one (see migration
// 000014's UNIQUE constraint).
//
// AuthorizedAction is set only when Outcome is ALLOW: it names the
// RecommendedAction the policy authorized to proceed to execution (a
// future milestone). It is empty for BLOCK/ESCALATE — nothing is
// authorized in those cases. An ALLOW here does NOT mean the action has
// executed; no RecoveryAction is created by this milestone.
type PolicyDecision struct {
	ID                           uuid.UUID
	RecoveryCaseID               uuid.UUID
	RecoveryDiagnosisID          uuid.UUID
	RecoveryEconomicEvaluationID uuid.UUID

	Outcome          PolicyDecisionOutcome
	AuthorizedAction RecommendedAction // empty unless Outcome == ALLOW
	PolicyVersion    string
	ReasonCodes      []PolicyReasonCode
	Explanation      string

	EvaluatedAt time.Time
	CreatedAt   time.Time
}
