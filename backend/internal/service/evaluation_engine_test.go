package service

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

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

	revguard, ok := result.Strategies["revguard"]
	if !ok {
		t.Fatal("missing revguard strategy result")
	}

	for _, baselineName := range []string{"fixed_retry", "static_rules"} {
		baseline, ok := result.Strategies[baselineName]
		if !ok {
			t.Fatalf("missing baseline %s", baselineName)
		}
		comparison, ok := result.Comparisons["vs_"+baselineName]
		if !ok {
			t.Fatalf("missing comparison vs_%s", baselineName)
		}

		wantIncremental := revguard.RevenueRecoveredMinorUnits - baseline.RevenueRecoveredMinorUnits
		if comparison.IncrementalRecoveredRevenueMinorUnits != wantIncremental {
			t.Fatalf("vs_%s: incremental recovered revenue mismatch: want %d got %d", baselineName, wantIncremental, comparison.IncrementalRecoveredRevenueMinorUnits)
		}

		wantNet := revguard.NetIncrementalValueMinorUnits - baseline.NetIncrementalValueMinorUnits
		if comparison.IncrementalNetValueMinorUnits != wantNet {
			t.Fatalf("vs_%s: incremental net value mismatch: want %d got %d", baselineName, wantNet, comparison.IncrementalNetValueMinorUnits)
		}

		wantReduction := actionReductionPercent(baseline.ActionsTaken, revguard.ActionsTaken)
		if comparison.ActionReductionPercent != wantReduction {
			t.Fatalf("vs_%s: action reduction mismatch: want %v got %v", baselineName, wantReduction, comparison.ActionReductionPercent)
		}
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

func TestRunEvaluation_AllThreeStrategiesPresent(t *testing.T) {
	result, err := RunEvaluation(1, 50)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}
	for _, name := range []string{"fixed_retry", "static_rules", "revguard"} {
		if _, ok := result.Strategies[name]; !ok {
			t.Fatalf("missing strategy %q in result", name)
		}
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
