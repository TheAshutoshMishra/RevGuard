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
	if !d.Executed {
		t.Fatal("fixed_retry assumes full execution capability and must be Executed on ALLOW")
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
	if !d.Executed {
		t.Fatal("static_rules assumes full execution capability and must be Executed on ALLOW")
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
	if blocked.Executed {
		t.Fatal("a BLOCK decision must never be Executed")
	}
}

// TestRevGuardStrategy_SendPaymentLinkIsNowExecuted covers Milestone
// 10's execution-fidelity change: send_payment_link is genuinely
// policy-ALLOWed (DefaultPolicyConfig.AutoAllowedActions has it set
// true) AND, as of Milestone 10, ExecutionEngine actually implements it
// (see executableActions in execution_engine.go). RevGuardStrategy must
// credit real cost/possible recovery for it now, unlike Milestone 8/9
// when only retry_payment was executable.
func TestRevGuardStrategy_SendPaymentLinkIsNowExecuted(t *testing.T) {
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
	if !d.Executed {
		t.Fatal("send_payment_link has a real execution implementation as of Milestone 10 and must be Executed")
	}
	if d.ActionCostMinorUnits <= 0 {
		t.Fatal("expected a positive action cost for an executed action")
	}
	if d.ExpectedGrossRecoveryMinorUnits <= 0 {
		t.Fatal("expected a positive ex-ante prediction from the Economic Engine")
	}
}

// TestRevGuardStrategy_UnsupportedActionAuthorizedButNotExecuted covers
// Milestone 9's execution-fidelity fix, still true for actions
// Milestone 10 did not add execution for: send_reminder can be
// genuinely policy-ALLOWed under the aggressive profile (its confidence
// floor of 0.50 is at or below deterministicDiagnosis's fixed 0.55 for
// customer_abandonment), but ExecutionEngine has no execution
// implementation for it. RevGuardStrategy must reflect that gap rather
// than silently crediting cost/recovery for an action that cannot
// actually run in production.
func TestRevGuardStrategy_UnsupportedActionAuthorizedButNotExecuted(t *testing.T) {
	s := NewRevGuardStrategyWithProfile("test_aggressive", AggressivePolicyConfig)
	opp := SyntheticOpportunity{
		AmountMinorUnits:        50_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryCustomerAbandonment,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)

	if d.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW (policy authorization is unaffected by execution capability), got %s", d.Outcome)
	}
	if d.Action != domain.RecommendedActionSendReminder {
		t.Fatalf("expected send_reminder, got %s", d.Action)
	}
	if d.Executed {
		t.Fatal("send_reminder has no execution implementation and must not be Executed")
	}
	if d.ActionCostMinorUnits != 0 || d.RiskCostMinorUnits != 0 {
		t.Fatalf("an unexecuted action must incur zero cost, got cost=%d risk=%d", d.ActionCostMinorUnits, d.RiskCostMinorUnits)
	}
	if d.ExpectedGrossRecoveryMinorUnits <= 0 {
		t.Fatal("the Economic Engine's ex-ante prediction must still be recorded even when the action can't execute")
	}
}

// TestRevGuardStrategy_EscalatesLowConfidence isolates the confidence
// rule (customer_abandonment -> send_reminder, confidence 0.55, which is
// itself an auto-allowed action) from the action-not-allowed rule
// exercised above, per the M9 stress scenario "low AI confidence".
func TestRevGuardStrategy_EscalatesLowConfidence(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        10_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryCustomerAbandonment,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)
	if d.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE for a low-confidence (0.55) recommendation, got %s", d.Outcome)
	}
	// Action is only populated on ALLOW (see StrategyDecision's doc
	// comment) — an ESCALATE decision authorizes nothing.
	if d.Executed {
		t.Fatal("an ESCALATE decision must never be Executed")
	}
}

func TestRevGuardStrategy_RetryPaymentIsExecuted(t *testing.T) {
	s := NewRevGuardStrategy()
	opp := SyntheticOpportunity{
		AmountMinorUnits:        50_000,
		Currency:                "INR",
		FailureCategory:         domain.FailureCategoryTransientFailure,
		PreviousAttempts:        1,
		PreviousRecoveryActions: 0,
	}
	d := mustDecide(t, s, opp)

	if d.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s", d.Outcome)
	}
	if d.Action != domain.RecommendedActionRetryPayment {
		t.Fatalf("expected retry_payment, got %s", d.Action)
	}
	if !d.Executed {
		t.Fatal("retry_payment is the one action Milestone 6 actually implements and must be Executed")
	}
	if d.ActionCostMinorUnits <= 0 {
		t.Fatal("an executed action must carry a positive cost")
	}
}
