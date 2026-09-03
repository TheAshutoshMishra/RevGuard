package service

// Internal (white-box) test file — package `service` — so it can call
// the unexported evaluatePolicyRules directly, mirroring
// economic_calculations_test.go's pattern for the same reason: these are
// pure-function tests that need no database.

import (
	"testing"

	"revguard/backend/internal/domain"
)

func mustMoney(t *testing.T, minorUnits int64, currency domain.Currency) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(minorUnits, currency)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return m
}

// safeDiagnosis and safeEvaluation together describe a case that should
// pass every rule and result in ALLOW. Each rule-specific test mutates
// exactly one field/count away from this baseline.
func safeDiagnosis(t *testing.T) *domain.RecoveryDiagnosis {
	return &domain.RecoveryDiagnosis{
		FailureCategory:   domain.FailureCategoryTransientFailure,
		RecommendedAction: domain.RecommendedActionRetryPayment,
		Confidence:        0.90,
	}
}

func safeEvaluation(t *testing.T) *domain.RecoveryEconomicEvaluation {
	return &domain.RecoveryEconomicEvaluation{
		RevenueAtRisk:                      mustMoney(t, 10_000, "INR"),
		ExpectedIncrementalValueMinorUnits: 5_000,
	}
}

func testConfig() PolicyConfig {
	return DefaultPolicyConfig
}

func TestPolicyRules_SafeCaseAllows(t *testing.T) {
	result := evaluatePolicyRules(testConfig(), PolicyRuleInput{
		Diagnosis:                safeDiagnosis(t),
		EconomicEvaluation:       safeEvaluation(t),
		PaymentAttemptCount:      0,
		PriorRecoveryActionCount: 0,
	})
	if result.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s (reasons=%v)", result.Outcome, result.ReasonCodes)
	}
	if len(result.ReasonCodes) != 1 || result.ReasonCodes[0] != domain.PolicyReasonPolicyAllowed {
		t.Fatalf("expected exactly [POLICY_ALLOWED], got %v", result.ReasonCodes)
	}
	if result.Explanation == "" {
		t.Fatal("expected a non-empty explanation")
	}
}

func TestPolicyRules_StopRecoveryBlocks(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	diagnosis.RecommendedAction = domain.RecommendedActionStopRecovery
	result := evaluatePolicyRules(testConfig(), PolicyRuleInput{
		Diagnosis: diagnosis, EconomicEvaluation: safeEvaluation(t),
	})
	if result.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonStopRecoveryRecommendation)
	// Rule (H) is deliberately skipped for stop_recovery — see
	// policy_rules.go — so ACTION_NOT_AUTO_ALLOWED must not also appear.
	assertNotContainsReason(t, result.ReasonCodes, domain.PolicyReasonActionNotAutoAllowed)
}

func TestPolicyRules_LowConfidenceEscalates(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	diagnosis.Confidence = 0.10
	result := evaluatePolicyRules(testConfig(), PolicyRuleInput{
		Diagnosis: diagnosis, EconomicEvaluation: safeEvaluation(t),
	})
	if result.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonLowAIConfidence)
}

func TestPolicyRules_NegativeExpectedValueBlocks(t *testing.T) {
	eval := safeEvaluation(t)
	eval.ExpectedIncrementalValueMinorUnits = -1
	result := evaluatePolicyRules(testConfig(), PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval,
	})
	if result.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonNegativeExpectedValue)
}

func TestPolicyRules_ZeroExpectedValueBlocksWhenMinimumIsPositive(t *testing.T) {
	config := testConfig()
	config.MinimumExpectedIncrementalValueMinorUnits = 1 // require strictly positive value
	eval := safeEvaluation(t)
	eval.ExpectedIncrementalValueMinorUnits = 0
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval,
	})
	if result.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK when minimum=1 and value=0, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonNegativeExpectedValue)
}

func TestPolicyRules_AmountAboveAutoLimitEscalates(t *testing.T) {
	config := testConfig()
	eval := safeEvaluation(t)
	eval.RevenueAtRisk = mustMoney(t, config.MaxAutoAmountMinorUnits+1, "INR")
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval,
	})
	if result.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonAmountAboveAutoLimit)
}

func TestPolicyRules_MaxPaymentAttemptsBlocks(t *testing.T) {
	config := testConfig()
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: safeEvaluation(t),
		PaymentAttemptCount: config.MaxPaymentAttempts,
	})
	if result.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonMaxAttemptsReached)
}

func TestPolicyRules_TooManyPriorActionsEscalates(t *testing.T) {
	config := testConfig()
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: safeEvaluation(t),
		PriorRecoveryActionCount: config.MaxPriorRecoveryActions,
	})
	if result.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonTooManyPriorActions)
}

func TestPolicyRules_ActionNotAutoAllowedEscalates(t *testing.T) {
	diagnosis := safeDiagnosis(t)
	diagnosis.RecommendedAction = domain.RecommendedActionEscalateToHuman // false in DefaultPolicyConfig
	result := evaluatePolicyRules(testConfig(), PolicyRuleInput{
		Diagnosis: diagnosis, EconomicEvaluation: safeEvaluation(t),
	})
	if result.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonActionNotAutoAllowed)
}

func TestPolicyRules_MultipleReasonsAllRecorded_BlockOutranksEscalate(t *testing.T) {
	config := testConfig()
	diagnosis := safeDiagnosis(t)
	diagnosis.Confidence = 0.10 // triggers ESCALATE (C)
	eval := safeEvaluation(t)
	eval.ExpectedIncrementalValueMinorUnits = -1 // triggers BLOCK (D)

	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: eval})

	if result.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK to outrank ESCALATE, got %s", result.Outcome)
	}
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonLowAIConfidence)
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonNegativeExpectedValue)
	if len(result.ReasonCodes) != 2 {
		t.Fatalf("expected exactly 2 reason codes, got %v", result.ReasonCodes)
	}
}

// --- Boundary tests -----------------------------------------------

func TestPolicyRules_Boundary_ConfidenceExactlyAtThreshold(t *testing.T) {
	config := testConfig()
	diagnosis := safeDiagnosis(t)
	diagnosis.Confidence = config.MinimumConfidence // exactly at threshold: must NOT escalate on this rule
	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: safeEvaluation(t)})
	assertNotContainsReason(t, result.ReasonCodes, domain.PolicyReasonLowAIConfidence)
}

func TestPolicyRules_Boundary_ConfidenceOneUnitBelowThreshold(t *testing.T) {
	config := testConfig()
	diagnosis := safeDiagnosis(t)
	diagnosis.Confidence = config.MinimumConfidence - 0.001
	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: safeEvaluation(t)})
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonLowAIConfidence)
}

func TestPolicyRules_Boundary_ExpectedValueExactlyAtThreshold(t *testing.T) {
	config := testConfig()
	eval := safeEvaluation(t)
	eval.ExpectedIncrementalValueMinorUnits = config.MinimumExpectedIncrementalValueMinorUnits
	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval})
	assertNotContainsReason(t, result.ReasonCodes, domain.PolicyReasonNegativeExpectedValue)
}

func TestPolicyRules_Boundary_ExpectedValueOneUnitBelowThreshold(t *testing.T) {
	config := testConfig()
	eval := safeEvaluation(t)
	eval.ExpectedIncrementalValueMinorUnits = config.MinimumExpectedIncrementalValueMinorUnits - 1
	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval})
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonNegativeExpectedValue)
}

func TestPolicyRules_Boundary_AmountExactlyAtAutoLimit(t *testing.T) {
	config := testConfig()
	eval := safeEvaluation(t)
	eval.RevenueAtRisk = mustMoney(t, config.MaxAutoAmountMinorUnits, "INR")
	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval})
	assertNotContainsReason(t, result.ReasonCodes, domain.PolicyReasonAmountAboveAutoLimit)
}

func TestPolicyRules_Boundary_AmountOneMinorUnitAboveAutoLimit(t *testing.T) {
	config := testConfig()
	eval := safeEvaluation(t)
	eval.RevenueAtRisk = mustMoney(t, config.MaxAutoAmountMinorUnits+1, "INR")
	result := evaluatePolicyRules(config, PolicyRuleInput{Diagnosis: safeDiagnosis(t), EconomicEvaluation: eval})
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonAmountAboveAutoLimit)
}

func TestPolicyRules_Boundary_AttemptsExactlyAtMaximum(t *testing.T) {
	config := testConfig()
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: safeEvaluation(t),
		PaymentAttemptCount: config.MaxPaymentAttempts,
	})
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonMaxAttemptsReached)
}

func TestPolicyRules_Boundary_AttemptsOneBelowMaximum(t *testing.T) {
	config := testConfig()
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: safeEvaluation(t),
		PaymentAttemptCount: config.MaxPaymentAttempts - 1,
	})
	assertNotContainsReason(t, result.ReasonCodes, domain.PolicyReasonMaxAttemptsReached)
}

func TestPolicyRules_Boundary_PriorActionsExactlyAtMaximum(t *testing.T) {
	config := testConfig()
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: safeEvaluation(t),
		PriorRecoveryActionCount: config.MaxPriorRecoveryActions,
	})
	assertContainsReason(t, result.ReasonCodes, domain.PolicyReasonTooManyPriorActions)
}

func TestPolicyRules_Boundary_PriorActionsOneBelowMaximum(t *testing.T) {
	config := testConfig()
	result := evaluatePolicyRules(config, PolicyRuleInput{
		Diagnosis: safeDiagnosis(t), EconomicEvaluation: safeEvaluation(t),
		PriorRecoveryActionCount: config.MaxPriorRecoveryActions - 1,
	})
	assertNotContainsReason(t, result.ReasonCodes, domain.PolicyReasonTooManyPriorActions)
}

func TestPolicyRules_Deterministic(t *testing.T) {
	config := testConfig()
	diagnosis := safeDiagnosis(t)
	eval := safeEvaluation(t)
	input := PolicyRuleInput{Diagnosis: diagnosis, EconomicEvaluation: eval, PaymentAttemptCount: 1, PriorRecoveryActionCount: 1}

	first := evaluatePolicyRules(config, input)
	second := evaluatePolicyRules(config, input)

	if first.Outcome != second.Outcome {
		t.Fatalf("non-deterministic outcome: %s then %s", first.Outcome, second.Outcome)
	}
	if len(first.ReasonCodes) != len(second.ReasonCodes) {
		t.Fatalf("non-deterministic reason codes: %v then %v", first.ReasonCodes, second.ReasonCodes)
	}
}

func assertContainsReason(t *testing.T, codes []domain.PolicyReasonCode, want domain.PolicyReasonCode) {
	t.Helper()
	for _, c := range codes {
		if c == want {
			return
		}
	}
	t.Errorf("expected reason codes %v to contain %s", codes, want)
}

func assertNotContainsReason(t *testing.T, codes []domain.PolicyReasonCode, notWant domain.PolicyReasonCode) {
	t.Helper()
	for _, c := range codes {
		if c == notWant {
			t.Errorf("expected reason codes %v to NOT contain %s", codes, notWant)
			return
		}
	}
}
