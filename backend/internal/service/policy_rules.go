package service

import (
	"fmt"
	"strings"

	"revguard/backend/internal/domain"
)

// PolicyRuleInput is everything the deterministic policy rules need. It
// deliberately contains no fields beyond what actually exists in the
// domain model — no invented consent/risk/gateway-capability fields.
type PolicyRuleInput struct {
	Diagnosis                *domain.RecoveryDiagnosis
	EconomicEvaluation       *domain.RecoveryEconomicEvaluation
	PaymentAttemptCount      int
	PriorRecoveryActionCount int
}

// PolicyRuleResult is the pure output of evaluating PolicyConfig's rules
// against a PolicyRuleInput.
type PolicyRuleResult struct {
	Outcome     domain.PolicyDecisionOutcome
	ReasonCodes []domain.PolicyReasonCode
	Explanation string
}

// evaluatePolicyRules is the entire Policy Engine decision logic: a
// small, deterministic set of comparisons, evaluated in a fixed order,
// with no I/O and no external calls (it is a pure function, unit-tested
// directly in policy_rules_test.go without any database).
//
// Every rule that applies is recorded, not just the first or most
// severe: PolicyRuleResult.ReasonCodes can contain more than one code
// when multiple rules fire, so a decision is fully explainable rather
// than hiding secondary reasons behind whichever rule happened to be
// checked first. The final Outcome is decided by severity, not by
// evaluation order: BLOCK outranks ESCALATE, which outranks ALLOW — so a
// case that would both BLOCK and ESCALATE for different reasons is
// correctly BLOCKed, with both reasons recorded.
//
// AI confidence and a positive expected economic value are inputs, never
// authorization: rule (C) and rule (D)/(E) below are two of several
// independent checks, not the whole decision, and none of them alone can
// produce ALLOW — ALLOW only happens when every rule fails to fire.
func evaluatePolicyRules(config PolicyConfig, input PolicyRuleInput) PolicyRuleResult {
	var reasons []domain.PolicyReasonCode
	blocked := false
	escalate := false

	action := input.Diagnosis.RecommendedAction

	// (B) A stop_recovery recommendation is always BLOCK: the AI itself
	// is saying no further recovery should be attempted.
	if action == domain.RecommendedActionStopRecovery {
		reasons = append(reasons, domain.PolicyReasonStopRecoveryRecommendation)
		blocked = true
	}

	// (C) AI confidence below the minimum: not authorization by itself
	// (see package doc), but below this floor the recommendation isn't
	// trusted enough to act on automatically.
	if input.Diagnosis.Confidence < config.MinimumConfidence {
		reasons = append(reasons, domain.PolicyReasonLowAIConfidence)
		escalate = true
	}

	// (D) Expected incremental value below the configured minimum
	// (which may be zero or positive, not only literally negative):
	// a positive economic evaluation is not authorization by itself
	// (see package doc) — this is one input among several, and a value
	// below the floor blocks regardless of how confident the AI was.
	if input.EconomicEvaluation.ExpectedIncrementalValueMinorUnits < config.MinimumExpectedIncrementalValueMinorUnits {
		reasons = append(reasons, domain.PolicyReasonNegativeExpectedValue)
		blocked = true
	}

	// (E) Revenue at risk above the automatic authorization ceiling.
	// This threshold also serves as the "human approval threshold" from
	// the milestone brief — see PolicyConfig.MaxAutoAmountMinorUnits's
	// doc comment for why those are treated as the same question here.
	if input.EconomicEvaluation.RevenueAtRisk.MinorUnits > config.MaxAutoAmountMinorUnits {
		reasons = append(reasons, domain.PolicyReasonAmountAboveAutoLimit)
		escalate = true
	}

	// (F) The underlying payment has been attempted too many times
	// already: further automatic recovery is blocked outright.
	if input.PaymentAttemptCount >= config.MaxPaymentAttempts {
		reasons = append(reasons, domain.PolicyReasonMaxAttemptsReached)
		blocked = true
	}

	// (G) Too many recovery actions already attempted on this case:
	// escalate for human judgment rather than blocking outright, since
	// unlike repeated raw payment attempts (F), repeated recovery
	// *strategies* having failed suggests the case needs a human
	// decision, not necessarily that recovery is hopeless.
	if input.PriorRecoveryActionCount >= config.MaxPriorRecoveryActions {
		reasons = append(reasons, domain.PolicyReasonTooManyPriorActions)
		escalate = true
	}

	// (H) The recommended action itself is not in the automatic
	// allow-list. Skipped when the action is stop_recovery: rule (B)
	// already covers that case with a more specific, clearer reason —
	// stop_recovery is never "auto allowed" by definition, so checking
	// this here too would just add a redundant, less specific reason
	// code to an already-BLOCKed decision.
	if action != domain.RecommendedActionStopRecovery && !config.AutoAllowedActions[action] {
		reasons = append(reasons, domain.PolicyReasonActionNotAutoAllowed)
		escalate = true
	}

	var outcome domain.PolicyDecisionOutcome
	switch {
	case blocked:
		outcome = domain.PolicyDecisionOutcomeBlock
	case escalate:
		outcome = domain.PolicyDecisionOutcomeEscalate
	default:
		outcome = domain.PolicyDecisionOutcomeAllow
		reasons = []domain.PolicyReasonCode{domain.PolicyReasonPolicyAllowed}
	}

	return PolicyRuleResult{
		Outcome:     outcome,
		ReasonCodes: reasons,
		Explanation: explainPolicyDecision(config, input, outcome, reasons),
	}
}

func explainPolicyDecision(config PolicyConfig, input PolicyRuleInput, outcome domain.PolicyDecisionOutcome, reasons []domain.PolicyReasonCode) string {
	codeStrs := make([]string, len(reasons))
	for i, c := range reasons {
		codeStrs[i] = string(c)
	}
	return fmt.Sprintf(
		"policy=%s decision=%s reasons=[%s] "+
			"recommended_action=%s auto_allowed=%v "+
			"confidence=%.3f (min=%.3f) "+
			"revenue_at_risk_minor_units=%d (max_auto=%d) "+
			"expected_incremental_value_minor_units=%d (min=%d) "+
			"payment_attempts=%d (max=%d) prior_actions=%d (max=%d)",
		config.Version, outcome, strings.Join(codeStrs, ","),
		input.Diagnosis.RecommendedAction, config.AutoAllowedActions[input.Diagnosis.RecommendedAction],
		input.Diagnosis.Confidence, config.MinimumConfidence,
		input.EconomicEvaluation.RevenueAtRisk.MinorUnits, config.MaxAutoAmountMinorUnits,
		input.EconomicEvaluation.ExpectedIncrementalValueMinorUnits, config.MinimumExpectedIncrementalValueMinorUnits,
		input.PaymentAttemptCount, config.MaxPaymentAttempts,
		input.PriorRecoveryActionCount, config.MaxPriorRecoveryActions,
	)
}
