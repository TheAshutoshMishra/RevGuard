package service

import "revguard/backend/internal/domain"

// PolicyVersion identifies this policy configuration and rule set as one
// versioned unit. Bump it whenever either changes in a way that would
// make past decisions non-reproducible under the new logic — the same
// discipline as EconomicModelVersion (Milestone 4).
const PolicyVersion = "policy-v1"

// PolicyConfig holds every threshold the deterministic policy rules
// compare against. All monetary fields are integer minor units; all
// probability-shaped fields (none currently) would be integer basis
// points, per project convention — nothing here is a float except
// MinimumConfidence, which mirrors RecoveryDiagnosis.Confidence's
// existing float64 type (Milestone 3) rather than introducing a second,
// inconsistent representation for the same concept.
//
// These thresholds are RevGuard's own illustrative demonstration
// defaults. They are not claimed production Razorpay policy, not derived
// from historical loss data, and not the product of any risk modeling —
// see docs/architecture/policy-engine.md.
type PolicyConfig struct {
	Version string

	// MinimumConfidence is compared against RecoveryDiagnosis.Confidence
	// (Milestone 3's existing float64 field — not redesigned here).
	// Below this, the recommendation is not trusted enough to authorize
	// automatically (PolicyReasonLowAIConfidence -> ESCALATE).
	MinimumConfidence float64

	// MaxAutoAmountMinorUnits is the ceiling on RevenueAtRisk for
	// automatic authorization. Above it, a human must review — this is
	// deliberately the same threshold the milestone brief calls both
	// "maximum automatic recovery amount" and "human approval threshold":
	// in this policy's smallest coherent model they are the same
	// question ("is this amount too large to auto-authorize?"), not two
	// independent limits with no data to distinguish them. See
	// docs/architecture/policy-engine.md for the rationale.
	MaxAutoAmountMinorUnits int64

	// MinimumExpectedIncrementalValueMinorUnits is the floor on
	// RecoveryEconomicEvaluation.ExpectedIncrementalValueMinorUnits.
	// Below it (including negative values), the action isn't worth
	// taking (PolicyReasonNegativeExpectedValue -> BLOCK).
	MinimumExpectedIncrementalValueMinorUnits int64

	// MaxPaymentAttempts: at or above this many attempts on the
	// underlying payment, further automatic recovery is blocked
	// (PolicyReasonMaxAttemptsReached -> BLOCK) — repeated attempts are
	// assumed to indicate a fundamentally failing payment method rather
	// than a transient issue.
	MaxPaymentAttempts int

	// MaxPriorRecoveryActions: at or above this many prior recovery
	// actions already attempted on the case, the case needs human
	// judgment rather than another automatic action
	// (PolicyReasonTooManyPriorActions -> ESCALATE).
	MaxPriorRecoveryActions int

	// AutoAllowedActions lists which RecommendedAction values the policy
	// permits to proceed automatically at all, independent of the other
	// checks. An action not in this map (or mapped to false) always
	// escalates (PolicyReasonActionNotAutoAllowed), regardless of
	// confidence/amount/attempts.
	AutoAllowedActions map[domain.RecommendedAction]bool
}

// DefaultPolicyConfig is the policy-v1 configuration used unless a
// caller supplies its own.
var DefaultPolicyConfig = PolicyConfig{
	Version:                 PolicyVersion,
	MinimumConfidence:       0.60,
	MaxAutoAmountMinorUnits: 100_000, // illustrative: e.g. INR 1,000.00
	MinimumExpectedIncrementalValueMinorUnits: 0,
	MaxPaymentAttempts:                        3,
	MaxPriorRecoveryActions:                   2,
	AutoAllowedActions: map[domain.RecommendedAction]bool{
		domain.RecommendedActionRetryPayment:               true,
		domain.RecommendedActionSendPaymentLink:            true,
		domain.RecommendedActionSendReminder:               true,
		domain.RecommendedActionRequestPaymentMethodChange: false, // requires human review in this default policy
		domain.RecommendedActionEscalateToHuman:            false, // already a request for human involvement
		domain.RecommendedActionStopRecovery:               false, // always BLOCK via the dedicated stop-recovery rule, never "auto allowed"
	},
}
