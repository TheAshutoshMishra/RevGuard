package service

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"revguard/backend/internal/domain"
)

func testDataset(opps []SyntheticOpportunity, truths []groundTruthResult) SyntheticDataset {
	return SyntheticDataset{Seed: 0, Type: "synthetic", Opportunities: opps, groundTruths: truths}
}

func TestAggregateStrategyMetrics_RecoveryRateFormula(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}, {AmountMinorUnits: 2000}},
		[]groundTruthResult{{Recoverable: true}, {Recoverable: false}},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
	}

	m := aggregateStrategyMetrics("test", dataset, decisions, 3000)

	if m.RevenueRecoveredMinorUnits != 1000 {
		t.Fatalf("expected recovered=1000, got %d", m.RevenueRecoveredMinorUnits)
	}
	want := 1000.0 / 3000.0
	if math.Abs(m.RecoveryRate-want) > 1e-9 {
		t.Fatalf("RecoveryRate = recovered/revenue_at_risk: expected %v, got %v", want, m.RecoveryRate)
	}
}

func TestAggregateStrategyMetrics_RecoveryRateZeroRevenueAtRisk(t *testing.T) {
	dataset := testDataset(nil, nil)
	m := aggregateStrategyMetrics("empty", dataset, nil, 0)
	if m.RecoveryRate != 0 {
		t.Fatalf("expected RecoveryRate 0 when revenue at risk is 0, got %v", m.RecoveryRate)
	}
}

func TestAggregateStrategyMetrics_NetIncrementalValueFormula(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}, {AmountMinorUnits: 2000}},
		[]groundTruthResult{{Recoverable: true}, {Recoverable: false}},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true, ActionCostMinorUnits: 100, RiskCostMinorUnits: 50},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true, ActionCostMinorUnits: 100, RiskCostMinorUnits: 50},
	}

	m := aggregateStrategyMetrics("test", dataset, decisions, 3000)

	wantRecovered := int64(1000)
	wantCost := int64(200)
	wantRisk := int64(100)
	wantNet := wantRecovered - wantCost - wantRisk

	if m.RevenueRecoveredMinorUnits != wantRecovered || m.RecoveryCostMinorUnits != wantCost || m.RiskCostMinorUnits != wantRisk {
		t.Fatalf("unexpected aggregates: %+v", m)
	}
	if m.NetIncrementalValueMinorUnits != wantNet {
		t.Fatalf("NetIncrementalValue = recovered - cost - risk: expected %d, got %d", wantNet, m.NetIncrementalValueMinorUnits)
	}
}

func TestAggregateStrategyMetrics_NetIncrementalValueCanBeNegative(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}},
		[]groundTruthResult{{Recoverable: false}},
	)
	decisions := []StrategyDecision{{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true, ActionCostMinorUnits: 500, RiskCostMinorUnits: 50}}

	m := aggregateStrategyMetrics("test", dataset, decisions, 1000)

	if m.NetIncrementalValueMinorUnits != -550 {
		t.Fatalf("expected negative net value -550, got %d", m.NetIncrementalValueMinorUnits)
	}
}

func TestAggregateStrategyMetrics_UnnecessaryActionsCounted(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}, {AmountMinorUnits: 1000}, {AmountMinorUnits: 1000}},
		[]groundTruthResult{{Recoverable: true}, {Recoverable: false}, {Recoverable: false}},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeBlock}, // never counted as unnecessary; no action taken
	}

	m := aggregateStrategyMetrics("test", dataset, decisions, 3000)

	if m.ActionsTaken != 2 {
		t.Fatalf("expected 2 actions taken, got %d", m.ActionsTaken)
	}
	if m.UnnecessaryActions != 1 {
		t.Fatalf("expected exactly 1 unnecessary action (taken but unrecovered), got %d", m.UnnecessaryActions)
	}
	if m.ActionsBlocked != 1 {
		t.Fatalf("expected 1 blocked action, got %d", m.ActionsBlocked)
	}
}

func TestAggregateStrategyMetrics_AllRecoverable(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}, {AmountMinorUnits: 2000}},
		[]groundTruthResult{{Recoverable: true}, {Recoverable: true}},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
	}
	m := aggregateStrategyMetrics("test", dataset, decisions, 3000)
	if m.RevenueRecoveredMinorUnits != 3000 {
		t.Fatalf("expected full recovery of 3000, got %d", m.RevenueRecoveredMinorUnits)
	}
	if m.UnnecessaryActions != 0 {
		t.Fatalf("expected 0 unnecessary actions when everything is recoverable, got %d", m.UnnecessaryActions)
	}
}

func TestAggregateStrategyMetrics_AllUnrecoverable(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}, {AmountMinorUnits: 2000}},
		[]groundTruthResult{{Recoverable: false}, {Recoverable: false}},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
	}
	m := aggregateStrategyMetrics("test", dataset, decisions, 3000)
	if m.RevenueRecoveredMinorUnits != 0 {
		t.Fatalf("expected 0 recovered when nothing is recoverable, got %d", m.RevenueRecoveredMinorUnits)
	}
	if m.UnnecessaryActions != 2 {
		t.Fatalf("expected every action to be unnecessary, got %d", m.UnnecessaryActions)
	}
}

func TestAggregateStrategyMetrics_NoOpportunities(t *testing.T) {
	dataset := testDataset(nil, nil)
	m := aggregateStrategyMetrics("empty", dataset, nil, 0)
	if m.ActionsTaken != 0 || m.RevenueRecoveredMinorUnits != 0 || m.AverageAttempts != 0 {
		t.Fatalf("expected all-zero metrics for an empty dataset, got %+v", m)
	}
}

func TestAggregateStrategyMetrics_LargeMonetaryValues(t *testing.T) {
	const largeAmount = int64(1_000_000_000_00) // INR 1,000,000,000.00
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: largeAmount}},
		[]groundTruthResult{{Recoverable: true}},
	)
	decisions := []StrategyDecision{{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true, ActionCostMinorUnits: 500, RiskCostMinorUnits: 200}}

	m := aggregateStrategyMetrics("test", dataset, decisions, largeAmount)

	if m.RevenueRecoveredMinorUnits != largeAmount {
		t.Fatalf("expected recovered == %d, got %d", largeAmount, m.RevenueRecoveredMinorUnits)
	}
	if m.NetIncrementalValueMinorUnits != largeAmount-700 {
		t.Fatalf("large-value arithmetic mismatch: got %d", m.NetIncrementalValueMinorUnits)
	}
}

func TestAggregateStrategyMetrics_UnsupportedActionsNoCostNoRecovery(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}},
		[]groundTruthResult{{Recoverable: true}},
	)
	decisions := []StrategyDecision{{
		Outcome:                         domain.PolicyDecisionOutcomeAllow,
		Executed:                        false, // authorized but not executable (Milestone 6 gap)
		Action:                          domain.RecommendedActionSendPaymentLink,
		ExpectedGrossRecoveryMinorUnits: 400,
	}}

	m := aggregateStrategyMetrics("test", dataset, decisions, 1000)

	if m.UnsupportedActions != 1 {
		t.Fatalf("expected 1 unsupported action, got %d", m.UnsupportedActions)
	}
	if m.ActionsTaken != 0 {
		t.Fatalf("an unexecuted action must not count as ActionsTaken, got %d", m.ActionsTaken)
	}
	if m.RevenueRecoveredMinorUnits != 0 || m.RecoveryCostMinorUnits != 0 || m.RiskCostMinorUnits != 0 {
		t.Fatalf("an unexecuted action must contribute zero revenue/cost, got %+v", m)
	}
	if m.ExpectedRecoveryValueMinorUnits != 400 {
		t.Fatalf("ExpectedRecoveryValue must still be recorded for an ALLOW decision, got %d", m.ExpectedRecoveryValueMinorUnits)
	}
	if m.UnnecessaryActions != 0 {
		t.Fatal("an unsupported (never executed) action is not the same as an unnecessary one")
	}
}

func TestAggregateStrategyMetrics_AmbiguousOutcomeNeverCountedRecoveredOrUnnecessary(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}},
		[]groundTruthResult{{Recoverable: true, ObservationAmbiguous: true}},
	)
	decisions := []StrategyDecision{{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true, ActionCostMinorUnits: 100}}

	m := aggregateStrategyMetrics("test", dataset, decisions, 1000)

	if m.AmbiguousOutcomes != 1 {
		t.Fatalf("expected 1 ambiguous outcome, got %d", m.AmbiguousOutcomes)
	}
	if m.RevenueRecoveredMinorUnits != 0 {
		t.Fatal("an ambiguous (UNKNOWN) outcome must never be counted as recovered, even though Recoverable was true")
	}
	if m.UnnecessaryActions != 0 {
		t.Fatal("an ambiguous outcome is unresolved, not definitively wasted — must not count as unnecessary")
	}
	if m.ActionsTaken != 1 {
		t.Fatalf("the action was genuinely executed and must count toward ActionsTaken, got %d", m.ActionsTaken)
	}
}

func TestAggregateStrategyMetrics_NoDoubleCounting(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{
			{AmountMinorUnits: 100}, // executed, SUCCESS
			{AmountMinorUnits: 100}, // executed, FAILED
			{AmountMinorUnits: 100}, // executed, UNKNOWN
			{AmountMinorUnits: 100}, // ALLOW but unsupported
			{AmountMinorUnits: 100}, // BLOCK
			{AmountMinorUnits: 100}, // ESCALATE
		},
		[]groundTruthResult{
			{Recoverable: true},
			{Recoverable: false},
			{Recoverable: true, ObservationAmbiguous: true},
			{Recoverable: true},
			{Recoverable: true},
			{Recoverable: true},
		},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true},
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: false},
		{Outcome: domain.PolicyDecisionOutcomeBlock},
		{Outcome: domain.PolicyDecisionOutcomeEscalate},
	}

	m := aggregateStrategyMetrics("test", dataset, decisions, 600)

	total := m.ActionsBlocked + m.HumanEscalations + m.UnsupportedActions +
		m.UnnecessaryActions + m.AmbiguousOutcomes
	successCount := 1 // exactly one SUCCESS opportunity above
	if total+successCount != len(dataset.Opportunities) {
		t.Fatalf("every opportunity must land in exactly one bucket: buckets sum to %d + 1 success != %d total", total, len(dataset.Opportunities))
	}
	if m.ActionsBlocked != 1 || m.HumanEscalations != 1 || m.UnsupportedActions != 1 || m.UnnecessaryActions != 1 || m.AmbiguousOutcomes != 1 {
		t.Fatalf("expected exactly one of each bucket, got %+v", m)
	}
	if m.RevenueRecoveredMinorUnits != 100 {
		t.Fatalf("expected exactly one opportunity's worth (100) recovered, got %d", m.RevenueRecoveredMinorUnits)
	}
	if m.ActionsTaken != 3 {
		t.Fatalf("expected 3 executed actions (SUCCESS, FAILED, UNKNOWN), got %d", m.ActionsTaken)
	}
}

func TestAggregateStrategyMetrics_ExpectedRecoveryValueOnlyOnAllow(t *testing.T) {
	dataset := testDataset(
		[]SyntheticOpportunity{{AmountMinorUnits: 1000}, {AmountMinorUnits: 1000}},
		[]groundTruthResult{{Recoverable: true}, {Recoverable: true}},
	)
	decisions := []StrategyDecision{
		{Outcome: domain.PolicyDecisionOutcomeAllow, Executed: true, ExpectedGrossRecoveryMinorUnits: 300},
		{Outcome: domain.PolicyDecisionOutcomeBlock, ExpectedGrossRecoveryMinorUnits: 999}, // must be ignored
	}
	m := aggregateStrategyMetrics("test", dataset, decisions, 2000)
	if m.ExpectedRecoveryValueMinorUnits != 300 {
		t.Fatalf("ExpectedRecoveryValue must only accumulate for ALLOW decisions, got %d", m.ExpectedRecoveryValueMinorUnits)
	}
}

// ---------------------------------------------------------------------
// Full RunEvaluation tests.
// ---------------------------------------------------------------------

func TestRunEvaluation_Reproducible(t *testing.T) {
	a, err := RunEvaluation(12345, 300)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	b, err := RunEvaluation(12345, 300)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}

	if !reflect.DeepEqual(a, b) {
		t.Fatal("RunEvaluation with the same seed/cases produced different results")
	}

	aJSON, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(aJSON) != string(bJSON) {
		t.Fatal("RunEvaluation JSON output is not byte-identical across runs with the same seed")
	}
}

// TestRunEvaluation_ProfilesReproducibleOnSameDatasetAndSeed is the
// Milestone 10 reproducibility requirement stated explicitly: same
// dataset + same seed + different policy profile must produce
// reproducible results — i.e. each individual profile's metrics, not
// just the whole EvaluationResult, are independently deterministic
// across repeated runs.
func TestRunEvaluation_ProfilesReproducibleOnSameDatasetAndSeed(t *testing.T) {
	a, err := RunEvaluation(777, 250)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	b, err := RunEvaluation(777, 250)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}

	for _, name := range []string{"revguard_conservative", "revguard_balanced", "revguard_aggressive"} {
		if a.Strategies[name] != b.Strategies[name] {
			t.Fatalf("%s: metrics not reproducible across runs: %+v vs %+v", name, a.Strategies[name], b.Strategies[name])
		}
	}
}

// TestRunEvaluation_ProfilesGenuinelyDiffer proves the three profiles
// are not accidentally identical: run against the exact same dataset,
// they must produce different aggregate metrics, since they have
// different thresholds (see TestPolicyProfiles_SameInputDifferentOutcomeAcrossProfiles
// for the underlying pure-function proof).
func TestRunEvaluation_ProfilesGenuinelyDiffer(t *testing.T) {
	result, err := RunEvaluation(2024, 500)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	conservative := result.Strategies["revguard_conservative"]
	balanced := result.Strategies["revguard_balanced"]
	aggressive := result.Strategies["revguard_aggressive"]

	if conservative == balanced && balanced == aggressive {
		t.Fatal("all three RevGuard profiles produced identical metrics — profiles are not actually differentiated")
	}
}

func TestRunEvaluation_DifferentSeedDifferentResult(t *testing.T) {
	a, err := RunEvaluation(1, 300)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	b, err := RunEvaluation(2, 300)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	if reflect.DeepEqual(a.Strategies, b.Strategies) {
		t.Fatal("different seeds produced identical strategy metrics (suspiciously non-random dataset)")
	}
}

func TestRunEvaluation_ZeroCases(t *testing.T) {
	result, err := RunEvaluation(1, 0)
	if err != nil {
		t.Fatalf("RunEvaluation with 0 cases should not error, got: %v", err)
	}
	if result.Dataset.Opportunities != 0 {
		t.Fatalf("expected 0 opportunities, got %d", result.Dataset.Opportunities)
	}
	for name, m := range result.Strategies {
		if m.ActionsTaken != 0 || m.RevenueRecoveredMinorUnits != 0 || m.RecoveryRate != 0 {
			t.Fatalf("strategy %s: expected all-zero metrics for an empty dataset, got %+v", name, m)
		}
	}
}

func TestRunEvaluation_NegativeCasesErrors(t *testing.T) {
	if _, err := RunEvaluation(1, -1); err == nil {
		t.Fatal("expected an error for a negative case count")
	}
}

func TestRunEvaluation_ComparisonsMatchStrategyMetrics(t *testing.T) {
	result, err := RunEvaluation(555, 400)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}

	profileNames := []string{"revguard_conservative", "revguard_balanced", "revguard_aggressive"}
	baselineNames := []string{"fixed_retry", "static_rules"}

	for _, profileName := range profileNames {
		profile, ok := result.Strategies[profileName]
		if !ok {
			t.Fatalf("missing %s strategy result", profileName)
		}

		for _, baselineName := range baselineNames {
			baseline, ok := result.Strategies[baselineName]
			if !ok {
				t.Fatalf("missing baseline %s", baselineName)
			}
			key := profileName + "_vs_" + baselineName
			comparison, ok := result.Comparisons[key]
			if !ok {
				t.Fatalf("missing comparison %s", key)
			}
			if comparison.ProfileName != profileName {
				t.Fatalf("%s: ProfileName mismatch: want %s got %s", key, profileName, comparison.ProfileName)
			}
			if comparison.BaselineName != baselineName {
				t.Fatalf("%s: BaselineName mismatch: want %s got %s", key, baselineName, comparison.BaselineName)
			}

			wantIncremental := profile.RevenueRecoveredMinorUnits - baseline.RevenueRecoveredMinorUnits
			if comparison.IncrementalRecoveredRevenueMinorUnits != wantIncremental {
				t.Fatalf("%s: incremental recovered revenue mismatch: want %d got %d", key, wantIncremental, comparison.IncrementalRecoveredRevenueMinorUnits)
			}

			wantNet := profile.NetIncrementalValueMinorUnits - baseline.NetIncrementalValueMinorUnits
			if comparison.IncrementalNetValueMinorUnits != wantNet {
				t.Fatalf("%s: incremental net value mismatch: want %d got %d", key, wantNet, comparison.IncrementalNetValueMinorUnits)
			}

			wantReduction := actionReductionPercent(baseline.ActionsTaken, profile.ActionsTaken)
			if comparison.ActionReductionPercent != wantReduction {
				t.Fatalf("%s: action reduction mismatch: want %v got %v", key, wantReduction, comparison.ActionReductionPercent)
			}

			wantRecoveryRateDelta := profile.RecoveryRate - baseline.RecoveryRate
			if comparison.IncrementalRecoveryRate != wantRecoveryRateDelta {
				t.Fatalf("%s: incremental recovery rate mismatch: want %v got %v", key, wantRecoveryRateDelta, comparison.IncrementalRecoveryRate)
			}
		}
	}

	if len(result.Comparisons) != len(profileNames)*len(baselineNames) {
		t.Fatalf("expected exactly %d comparisons, got %d", len(profileNames)*len(baselineNames), len(result.Comparisons))
	}
}

func TestRunEvaluation_DisclaimerPresent(t *testing.T) {
	result, err := RunEvaluation(1, 10)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	if !strings.Contains(result.Disclaimer, "SYNTHETIC") {
		t.Fatal("disclaimer must clearly identify the dataset as synthetic")
	}
	if !strings.Contains(result.Disclaimer, "NOT") {
		t.Fatal("disclaimer must explicitly deny live Razorpay validation")
	}
	if result.Dataset.Type != "synthetic" {
		t.Fatalf("dataset type must be \"synthetic\", got %q", result.Dataset.Type)
	}
}

func TestRunEvaluation_AllFiveStrategiesPresent(t *testing.T) {
	result, err := RunEvaluation(1, 50)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	want := []string{"fixed_retry", "static_rules", "revguard_conservative", "revguard_balanced", "revguard_aggressive"}
	for _, name := range want {
		if _, ok := result.Strategies[name]; !ok {
			t.Fatalf("missing strategy %q in result", name)
		}
	}
	if len(result.Strategies) != len(want) {
		t.Fatalf("expected exactly %d strategies, got %d", len(want), len(result.Strategies))
	}
}

func TestFormatMarkdownReport_ContainsRequiredSections(t *testing.T) {
	result, err := RunEvaluation(42, 50)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	report := FormatMarkdownReport(result, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), "abc1234")

	for _, want := range []string{
		"# RevGuard Evaluation Report",
		"Synthetic evaluation — not production performance.",
		"2026-09-03T12:00:00Z",
		"abc1234",
		"Seed: `42`",
		"Scenario count: `50`",
		"## Assumptions",
		"## Strategy definitions",
		"## Results",
		"## Comparison (each RevGuard profile vs. each baseline)",
		"## Limitations",
		"fixed_retry", "static_rules", "revguard_conservative", "revguard_balanced", "revguard_aggressive",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("markdown report missing %q:\n%s", want, report)
		}
	}
}

func TestFormatMarkdownReport_EmptyCommitDefaultsToUnknown(t *testing.T) {
	result, err := RunEvaluation(1, 10)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	report := FormatMarkdownReport(result, time.Now(), "")
	if !strings.Contains(report, "`unknown`") {
		t.Fatal("expected an empty commit to render as `unknown`")
	}
}

func TestFormatResultTable_ContainsKeyData(t *testing.T) {
	result, err := RunEvaluation(1, 50)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	table := FormatResultTable(result)
	for _, want := range []string{"fixed_retry", "static_rules", "revguard", "SYNTHETIC", "NOT live Razorpay data"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table output missing %q:\n%s", want, table)
		}
	}
}
