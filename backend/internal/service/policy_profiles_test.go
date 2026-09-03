package service

// Internal (white-box) test file — package `service` — mirroring
// policy_rules_test.go's pattern: pure-function tests over
// evaluatePolicyRules with different PolicyConfig profiles, no database.

import (
	"reflect"
	"testing"

	"revguard/backend/internal/domain"
)

func TestPolicyProfiles_RegistryHasExactlyThreeProfiles(t *testing.T) {
	want := map[string]PolicyConfig{
		PolicyProfileConservative: ConservativePolicyConfig,
		PolicyProfileBalanced:     BalancedPolicyConfig,
		PolicyProfileAggressive:   AggressivePolicyConfig,
	}
	if len(PolicyProfiles) != len(want) {
		t.Fatalf("expected %d profiles, got %d", len(want), len(PolicyProfiles))
	}
	for key, config := range want {
		got, ok := PolicyProfiles[key]
		if !ok {
			t.Fatalf("missing profile %q", key)
		}
		if !reflect.DeepEqual(got, config) {
			t.Fatalf("profile %q does not match its named variable", key)
		}
	}
}

func TestBalancedPolicyConfig_MatchesDefaultPolicyConfig(t *testing.T) {
	if !reflect.DeepEqual(BalancedPolicyConfig, DefaultPolicyConfig) {
		t.Fatal("BalancedPolicyConfig must be numerically identical to DefaultPolicyConfig — Milestone 10 must not change existing production behavior")
	}
}

func TestPolicyProfiles_EveryProfileHasValidThresholds(t *testing.T) {
	for name, config := range PolicyProfiles {
		if config.MinimumConfidence <= 0 || config.MinimumConfidence >= 1 {
			t.Errorf("%s: MinimumConfidence out of (0,1): %v", name, config.MinimumConfidence)
		}
		if config.MaxAutoAmountMinorUnits <= 0 {
			t.Errorf("%s: MaxAutoAmountMinorUnits must be positive, got %d", name, config.MaxAutoAmountMinorUnits)
		}
		// Non-negotiable safety invariant regardless of profile: no
		// profile may authorize execution with a computed negative
		// expected value. "Aggressive" means more tolerant thresholds
		// elsewhere, never a negative floor here.
		if config.MinimumExpectedIncrementalValueMinorUnits < 0 {
			t.Errorf("%s: MinimumExpectedIncrementalValueMinorUnits must never be negative, got %d", name, config.MinimumExpectedIncrementalValueMinorUnits)
		}
		if config.MaxPaymentAttempts <= 0 {
			t.Errorf("%s: MaxPaymentAttempts must be positive, got %d", name, config.MaxPaymentAttempts)
		}
		if config.MaxPriorRecoveryActions <= 0 {
			t.Errorf("%s: MaxPriorRecoveryActions must be positive, got %d", name, config.MaxPriorRecoveryActions)
		}
		// stop_recovery must never be auto-allowed by any profile — rule
		// (B) in evaluatePolicyRules already BLOCKs it unconditionally,
		// but the config itself should never claim otherwise either.
		if config.AutoAllowedActions[domain.RecommendedActionStopRecovery] {
			t.Errorf("%s: stop_recovery must never be in AutoAllowedActions", name)
		}
	}
}

func TestPolicyProfiles_StopRecoveryAlwaysBlockedRegardlessOfProfile(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	diagnosis.RecommendedAction = domain.RecommendedActionStopRecovery
	evaluation := safeEvaluation(t)

	for name, config := range PolicyProfiles {
		result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: evaluation})
		if result.Outcome != domain.PolicyDecisionOutcomeBlock {
			t.Errorf("%s: expected BLOCK for stop_recovery, got %s", name, result.Outcome)
		}
	}
}

func TestPolicyProfiles_NegativeExpectedValueAlwaysBlockedRegardlessOfProfile(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	evaluation := safeEvaluation(t)
	evaluation.ExpectedIncrementalValueMinorUnits = -1

	for name, config := range PolicyProfiles {
		result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: evaluation})
		if result.Outcome != domain.PolicyDecisionOutcomeBlock {
			t.Errorf("%s: expected BLOCK for negative expected value, got %s", name, result.Outcome)
		}
	}
}

func TestPolicyProfiles_ConfidenceAloneNeverAuthorizes(t *testing.T) {
	// High confidence alone, paired with a negative expected value, must
	// still BLOCK under every profile — proving confidence never
	// overrides the economic gate.
	diagnosis := safeDiagnosis(t)
	diagnosis.Confidence = 0.999
	evaluation := safeEvaluation(t)
	evaluation.ExpectedIncrementalValueMinorUnits = -1

	for name, config := range PolicyProfiles {
		result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: evaluation})
		if result.Outcome == domain.PolicyDecisionOutcomeAllow {
			t.Errorf("%s: high confidence must not authorize a negative-expected-value action", name)
		}
	}
}

func TestPolicyProfiles_ExpectedValueAloneNeverAuthorizes(t *testing.T) {
	// A strongly positive expected value, paired with confidence below
	// every profile's floor, must still not ALLOW.
	diagnosis := safeDiagnosis(t)
	diagnosis.Confidence = 0.01
	evaluation := safeEvaluation(t)
	evaluation.ExpectedIncrementalValueMinorUnits = 1_000_000

	for name, config := range PolicyProfiles {
		result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: evaluation})
		if result.Outcome == domain.PolicyDecisionOutcomeAllow {
			t.Errorf("%s: a large positive expected value must not authorize a low-confidence action", name)
		}
	}
}

// TestPolicyProfiles_SameInputDifferentOutcomeAcrossProfiles is the
// direct demonstration that profiles express genuinely different risk
// appetites on identical input: an amount above Conservative's ceiling
// but at/under Aggressive's escalates under one and is allowed under the
// other, using the exact same diagnosis/evaluation values (deterministic
// — no randomness involved).
func TestPolicyProfiles_SameInputDifferentOutcomeAcrossProfiles(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	evaluation := &domain.RecoveryEconomicEvaluation{
		RevenueAtRisk:                      mustMoney(t, 200_000, "INR"), // above conservative(50k)/balanced(100k), at/under aggressive(300k)
		ExpectedIncrementalValueMinorUnits: 5_000,
	}
	input := PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: evaluation}

	conservative := evaluatePolicyRules(ConservativePolicyConfig, input)
	balanced := evaluatePolicyRules(BalancedPolicyConfig, input)
	aggressive := evaluatePolicyRules(AggressivePolicyConfig, input)

	if conservative.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected conservative to ESCALATE above its amount ceiling, got %s", conservative.Outcome)
	}
	if balanced.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected balanced to ESCALATE above its amount ceiling, got %s", balanced.Outcome)
	}
	if aggressive.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected aggressive to ALLOW within its higher amount ceiling, got %s", aggressive.Outcome)
	}
}

// TestPolicyProfiles_Deterministic proves evaluatePolicyRules under any
// profile is a pure function: identical input, identical output, called
// twice.
func TestPolicyProfiles_Deterministic(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	evaluation := safeEvaluation(t)
	input := PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: evaluation}

	for name, config := range PolicyProfiles {
		first := evaluatePolicyRules(config, input)
		second := evaluatePolicyRules(config, input)
		if first.Outcome != second.Outcome || !reflect.DeepEqual(first.ReasonCodes, second.ReasonCodes) {
			t.Errorf("%s: evaluatePolicyRules is not deterministic: %+v vs %+v", name, first, second)
		}
	}
}
