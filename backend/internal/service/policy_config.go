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
// caller supplies its own. It is identical to BalancedPolicyConfig
// below (Milestone 10) — kept as its own variable, rather than an alias,
// so nothing about M0–M9's production wiring (`cmd/server/main.go`,
// every pre-Milestone-10 test) has to change to keep working exactly as
// before.
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

// ---------------------------------------------------------------------
// Policy profiles (Milestone 10).
//
// A merchant's risk appetite varies — the same deterministic rule set
// (evaluatePolicyRules, unchanged since Milestone 5) can be tuned toward
// more or less automatic authorization purely by adjusting these
// threshold VALUES, never by changing the RULES themselves. Every
// profile below still runs every rule (B) through (H); none of them can
// let AI confidence or a positive expected value alone authorize
// execution (rules (C) and (D)/(E) remain independent checks among
// several), and stop_recovery is still unconditionally BLOCKed by rule
// (B) regardless of any profile's AutoAllowedActions map (that
// enforcement lives in evaluatePolicyRules's code, not in config, so no
// profile can weaken it).
//
// These are RevGuard's own illustrative demonstration configurations —
// not claimed production Razorpay policy, not derived from historical
// loss data, and not the product of real risk modeling, exactly like
// DefaultPolicyConfig itself (see docs/architecture/policy-engine.md).
// They exist to let the Milestone 9/10 evaluation harness (and, if
// POLICY_PROFILE is set, cmd/server itself) express "how automated
// should this merchant's recovery be" as one explicit choice among
// three, rather than a single hardcoded number.
// ---------------------------------------------------------------------

// ConservativePolicyConfig favors human review over automation: a higher
// confidence bar, a lower auto-approval ceiling, a required positive
// (not just non-negative) expected value buffer, and fewer automatic
// attempts before requiring a human decision. Uses the same
// AutoAllowedActions allow-list as the default/balanced profile —
// conservatism here comes entirely from the numeric thresholds, not from
// further restricting which actions could ever qualify.
var ConservativePolicyConfig = PolicyConfig{
	Version:                 "policy-v1-conservative",
	MinimumConfidence:       0.75,
	MaxAutoAmountMinorUnits: 50_000, // illustrative: e.g. INR 500.00
	MinimumExpectedIncrementalValueMinorUnits: 500,
	MaxPaymentAttempts:                        2,
	MaxPriorRecoveryActions:                   1,
	AutoAllowedActions: map[domain.RecommendedAction]bool{
		domain.RecommendedActionRetryPayment:               true,
		domain.RecommendedActionSendPaymentLink:            true,
		domain.RecommendedActionSendReminder:               true,
		domain.RecommendedActionRequestPaymentMethodChange: false,
		domain.RecommendedActionEscalateToHuman:            false,
		domain.RecommendedActionStopRecovery:               false,
	},
}

// BalancedPolicyConfig is RevGuard's existing policy-v1 default,
// unchanged by Milestone 10 — see DefaultPolicyConfig's doc comment for
// why these are two separate variables with identical values rather than
// one being an alias of the other.
var BalancedPolicyConfig = PolicyConfig{
	Version:                 PolicyVersion,
	MinimumConfidence:       0.60,
	MaxAutoAmountMinorUnits: 100_000,
	MinimumExpectedIncrementalValueMinorUnits: 0,
	MaxPaymentAttempts:                        3,
	MaxPriorRecoveryActions:                   2,
	AutoAllowedActions: map[domain.RecommendedAction]bool{
		domain.RecommendedActionRetryPayment:               true,
		domain.RecommendedActionSendPaymentLink:            true,
		domain.RecommendedActionSendReminder:               true,
		domain.RecommendedActionRequestPaymentMethodChange: false,
		domain.RecommendedActionEscalateToHuman:            false,
		domain.RecommendedActionStopRecovery:               false,
	},
}

// AggressivePolicyConfig favors automation over human review: a lower
// (but still nonzero — confidence never becomes sufficient on its own)
// confidence bar, a higher auto-approval ceiling, more attempts/prior
// actions tolerated before requiring a human decision, and one
// additional auto-allowed action (request_payment_method_change) for a
// merchant willing to let that customer-facing prompt run automatically.
// MinimumExpectedIncrementalValueMinorUnits is deliberately still 0, not
// negative, in every profile including this one: no profile is allowed
// to auto-authorize an action with a computed negative expected value
// (see rule (D) in evaluatePolicyRules) — "aggressive" means more
// tolerant thresholds, never a weakened safety rule. stop_recovery is
// still unconditionally BLOCKed regardless of this map, for the same
// reason noted above.
var AggressivePolicyConfig = PolicyConfig{
	Version:                 "policy-v1-aggressive",
	MinimumConfidence:       0.50,
	MaxAutoAmountMinorUnits: 300_000, // illustrative: e.g. INR 3,000.00
	MinimumExpectedIncrementalValueMinorUnits: 0,
	MaxPaymentAttempts:                        4,
	MaxPriorRecoveryActions:                   3,
	AutoAllowedActions: map[domain.RecommendedAction]bool{
		domain.RecommendedActionRetryPayment:               true,
		domain.RecommendedActionSendPaymentLink:            true,
		domain.RecommendedActionSendReminder:               true,
		domain.RecommendedActionRequestPaymentMethodChange: true,
		domain.RecommendedActionEscalateToHuman:            false,
		domain.RecommendedActionStopRecovery:               false,
	},
}

// PolicyProfileConservative/Balanced/Aggressive are the stable string
// keys used to select a profile (POLICY_PROFILE env var, evaluation CLI,
// PolicyProfiles lookup map below).
const (
	PolicyProfileConservative = "conservative"
	PolicyProfileBalanced     = "balanced"
	PolicyProfileAggressive   = "aggressive"
)

// PolicyProfiles maps each stable profile key to its PolicyConfig, for
// callers that select a profile by name (cmd/server/main.go's
// POLICY_PROFILE env var, the Milestone 9/10 evaluation harness). An
// unrecognized key is the caller's responsibility to reject — this map
// deliberately has no "unknown" fallback entry, so a typo fails loudly
// rather than silently defaulting to some profile.
var PolicyProfiles = map[string]PolicyConfig{
	PolicyProfileConservative: ConservativePolicyConfig,
	PolicyProfileBalanced:     BalancedPolicyConfig,
	PolicyProfileAggressive:   AggressivePolicyConfig,
}
