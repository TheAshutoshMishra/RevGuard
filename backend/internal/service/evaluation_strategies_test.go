package service

import (
	"testing"

	"revguard/backend/internal/domain"
)

func mustDecide(t *testing.T, strat EvaluationStrategy, opp SyntheticOpportunity) StrategyDecision {
	t.Helper()
	d, err := strat.Decide(opp)
	if err != nil {
		t.Fatalf("%s.Decide failed: %v", strat.Name(), err)
	}
	return d
}

func TestEvaluationStrategies_Deterministic(t *testing.T) {
	opp := SyntheticOpportunity{
		ID:               "SYN-000001",
		AmountMinorUnits: 50_000,
		Currency:         "INR",
		FailureCategory:  domain.FailureCategoryInsufficientFunds,
		PreviousAttempts: 1,
	}
	for _, strat := range []EvaluationStrategy{NewFixedRetryStrategy(), NewStaticRulesStrategy(), NewRevGuardStrategy()} {
		first := mustDecide(t, strat, opp)
		second := mustDecide(t, strat, opp)
		if first != second {
			t.Fatalf("%s.Decide is not deterministic: %+v vs %+v", strat.Name(), first, second)
		}
	}
}

func TestFixedRetryStrategy_RetriesUnderMaxAttempts(t *testing.T) {
	s := NewFixedRetryStrategy()
	opp := SyntheticOpportunity{AmountMinorUnits: 10_000, PreviousAttempts: 1}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s", d.Outcome)
	}
	if d.Action != domain.RecommendedActionRetryPayment {
		t.Fatalf("expected retry_payment, got %s", d.Action)
	}
	if d.ActionCostMinorUnits <= 0 {
		t.Fatal("expected positive action cost")
	}
}

func TestFixedRetryStrategy_BlocksAtMaxAttempts(t *testing.T) {
	s := NewFixedRetryStrategy()
	opp := SyntheticOpportunity{AmountMinorUnits: 10_000, PreviousAttempts: s.MaxAttempts}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK at max attempts, got %s", d.Outcome)
	}
	if d.ActionCostMinorUnits != 0 {
		t.Fatal("a blocked decision must incur zero cost")
	}
}

func TestFixedRetryStrategy_NeverEscalates(t *testing.T) {
	s := NewFixedRetryStrategy()
	for attempts := 0; attempts < 10; attempts++ {
		d := mustDecide(t, s, SyntheticOpportunity{AmountMinorUnits: 1, PreviousAttempts: attempts})
		if d.Outcome == domain.PolicyDecisionOutcomeEscalate {
			t.Fatal("fixed_retry has no escalation concept and must never escalate")
		}
	}
}

func TestStaticRulesStrategy_AllowsQualifyingCategory(t *testing.T) {
	s := NewStaticRulesStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:  10_000,
		FailureCategory:   domain.FailureCategoryTransientFailure,
		PreviousAttempts:  0,
		HoursSinceFailure: s.CooldownHours,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s", d.Outcome)
	}
	if d.Action != domain.RecommendedActionRetryPayment {
		t.Fatalf("expected retry_payment for transient_failure, got %s", d.Action)
	}
}

func TestStaticRulesStrategy_BlocksUnlistedCategory(t *testing.T) {
	s := NewStaticRulesStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:  10_000,
		FailureCategory:   domain.FailureCategoryMandateIssue,
		HoursSinceFailure: s.CooldownHours,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK for an unlisted category, got %s", d.Outcome)
	}
}

func TestStaticRulesStrategy_BlocksBeforeCooldown(t *testing.T) {
	s := NewStaticRulesStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:  10_000,
		FailureCategory:   domain.FailureCategoryTransientFailure,
		HoursSinceFailure: s.CooldownHours - 1,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK before cooldown elapses, got %s", d.Outcome)
	}
}

func TestStaticRulesStrategy_BlocksAboveAmountThreshold(t *testing.T) {
	s := NewStaticRulesStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:  s.MaxAmountMinorUnits + 1,
		FailureCategory:   domain.FailureCategoryTransientFailure,
		HoursSinceFailure: s.CooldownHours,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK above the amount threshold, got %s", d.Outcome)
	}
}

func TestStaticRulesStrategy_NeverEscalates(t *testing.T) {
	s := NewStaticRulesStrategy()
	for _, cat := range domain.ValidFailureCategories {
		d := mustDecide(t, s, SyntheticOpportunity{AmountMinorUnits: 10, FailureCategory: cat, HoursSinceFailure: 100})
		if d.Outcome == domain.PolicyDecisionOutcomeEscalate {
			t.Fatal("static_rules has no escalation concept and must never escalate")
		}
	}
}

func TestRevGuardStrategy_AllowsLowAmountInsufficientFunds(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        50_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryInsufficientFunds,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s", d.Outcome)
	}
	if d.Action != domain.RecommendedActionSendPaymentLink {
		t.Fatalf("expected send_payment_link, got %s", d.Action)
	}
}

func TestRevGuardStrategy_EscalatesAboveAutoAmountLimit(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        DefaultPolicyConfig.MaxAutoAmountMinorUnits + 1,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryInsufficientFunds,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE above the auto amount limit, got %s", d.Outcome)
	}
}

func TestRevGuardStrategy_BlocksAtMaxPaymentAttempts(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        50_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryInsufficientFunds,
		PreviousAttempts:        DefaultPolicyConfig.MaxPaymentAttempts,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK at max payment attempts, got %s", d.Outcome)
	}
}

func TestRevGuardStrategy_BlocksStopRecoveryRecommendation(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        10_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryTransientFailure,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 2, // triggers deterministicDiagnosis's stop_recovery branch
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK for a stop_recovery recommendation, got %s", d.Outcome)
	}
}

func TestRevGuardStrategy_EscalatesActionNotAutoAllowed(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        10_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryAuthenticationIssue,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE for request_payment_method_change (not auto-allowed), got %s", d.Outcome)
	}
}

func TestRevGuardStrategy_AllowOnlySetsCostWhenAllowed(t *testing.T) {
	s := NewRevGuardStrategy()
	blocked := mustDecide(t, s, SyntheticOpportunity{
		AmountMinorUnits: 10_000, FailureCategory: domain.FailureCategoryTransientFailure,
		PreviousAttempts: 1, PreviousRecoveryActions: 2,
	})
	if blocked.ActionCostMinorUnits != 0 || blocked.RiskCostMinorUnits != 0 {
		t.Fatal("a BLOCK decision must carry zero cost")
	}
}
