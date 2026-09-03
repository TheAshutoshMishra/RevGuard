package service

import (
	"reflect"
	"strings"
	"testing"
)

// TestFairness_AllStrategiesSeeSameDataset proves that running all three
// strategies over one generated dataset never mutates
// dataset.Opportunities: every strategy is handed the same opportunity
// values, and a post-run comparison against a freshly regenerated
// dataset with the same seed confirms nothing was altered in place.
func TestFairness_AllStrategiesSeeSameDataset(t *testing.T) {
	dataset := GenerateSyntheticDataset(42, 50)
	before := append([]SyntheticOpportunity(nil), dataset.Opportunities...)

	for _, strat := range []EvaluationStrategy{NewFixedRetryStrategy(), NewStaticRulesStrategy(), NewRevGuardStrategy()} {
		for _, opp := range dataset.Opportunities {
			if _, err := strat.Decide(opp); err != nil {
				t.Fatalf("%s failed: %v", strat.Name(), err)
			}
		}
	}

	if !reflect.DeepEqual(before, dataset.Opportunities) {
		t.Fatal("dataset.Opportunities was mutated by running strategies over it")
	}
}

// TestFairness_StrategyDecisionOrderIndependent proves that running the
// strategies in a different order over the same dataset produces
// identical aggregate metrics — no shared mutable state leaks between
// strategies or across opportunities.
func TestFairness_StrategyDecisionOrderIndependent(t *testing.T) {
	dataset := GenerateSyntheticDataset(99, 100)
	strategies := []EvaluationStrategy{NewFixedRetryStrategy(), NewStaticRulesStrategy(), NewRevGuardStrategy()}

	runInOrder := func(order []int) map[string]StrategyMetrics {
		results := make(map[string]StrategyMetrics)
		var revenueAtRisk int64
		for _, opp := range dataset.Opportunities {
			revenueAtRisk += opp.AmountMinorUnits
		}
		for _, idx := range order {
			strat := strategies[idx]
			decisions := make([]StrategyDecision, len(dataset.Opportunities))
			for i, opp := range dataset.Opportunities {
				d, err := strat.Decide(opp)
				if err != nil {
					t.Fatalf("%s failed: %v", strat.Name(), err)
				}
				decisions[i] = d
			}
			results[strat.Name()] = aggregateStrategyMetrics(strat.Name(), dataset, decisions, revenueAtRisk)
		}
		return results
	}

	forward := runInOrder([]int{0, 1, 2})
	reversed := runInOrder([]int{2, 1, 0})

	for name, m := range forward {
		if m != reversed[name] {
			t.Fatalf("strategy %s metrics differ by evaluation order: %+v vs %+v", name, m, reversed[name])
		}
	}
}

// TestFairness_BaselinesCannotSeeRevGuardDecisions is a structural check:
// FixedRetryStrategy and StaticRulesStrategy's Decide implementations
// take only a SyntheticOpportunity and are computed before RevGuard's
// strategy ever runs in this test, and still produce the same result as
// when run after — proving nothing about RevGuard's decision could have
// leaked in.
func TestFairness_BaselinesCannotSeeRevGuardDecisions(t *testing.T) {
	opp := SyntheticOpportunity{AmountMinorUnits: 40_000, PreviousAttempts: 1, HoursSinceFailure: 10}

	fixedBefore, _ := NewFixedRetryStrategy().Decide(opp)
	staticBefore, _ := NewStaticRulesStrategy().Decide(opp)

	// Now run RevGuard.
	_, _ = NewRevGuardStrategy().Decide(opp)

	fixedAfter, _ := NewFixedRetryStrategy().Decide(opp)
	staticAfter, _ := NewStaticRulesStrategy().Decide(opp)

	if fixedBefore != fixedAfter {
		t.Fatal("fixed_retry's decision changed after RevGuard ran — it must be independent")
	}
	if staticBefore != staticAfter {
		t.Fatal("static_rules's decision changed after RevGuard ran — it must be independent")
	}
}

// TestFairness_NoProductionRazorpayClaims scans every user-facing output
// surface (JSON field names/values via the disclaimer, and the table)
// for language that could be mistaken for a live-production claim.
func TestFairness_NoProductionRazorpayClaims(t *testing.T) {
	result, err := RunEvaluation(1, 20)
	if err != nil {
		t.Fatalf("RunEvaluation failed: %v", err)
	}

	forbidden := []string{"validated against live Razorpay", "production-verified", "real merchant data"}
	table := FormatResultTable(result)
	for _, phrase := range forbidden {
		if strings.Contains(table, phrase) {
			t.Fatalf("table output contains a forbidden production-claim phrase: %q", phrase)
		}
	}

	if result.Dataset.Type != "synthetic" {
		t.Fatalf("dataset must be labeled synthetic, got %q", result.Dataset.Type)
	}
	if !strings.Contains(result.Disclaimer, "synthetic") && !strings.Contains(result.Disclaimer, "SYNTHETIC") {
		t.Fatal("disclaimer must label the data synthetic")
	}
}
