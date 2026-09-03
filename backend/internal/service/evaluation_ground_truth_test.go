package service

import (
	"math/rand"
	"testing"

	"revguard/backend/internal/domain"
)

func TestComputeGroundTruth_Deterministic(t *testing.T) {
	opp := SyntheticOpportunity{
		FailureCategory:  domain.FailureCategoryTransientFailure,
		PaymentMethod:    "upi",
		PreviousAttempts: 1,
	}
	a := computeGroundTruth(opp, rand.New(rand.NewSource(42)))
	b := computeGroundTruth(opp, rand.New(rand.NewSource(42)))
	if a != b {
		t.Fatalf("computeGroundTruth is not deterministic for a fixed rand source: %+v vs %+v", a, b)
	}
}

func TestComputeGroundTruth_ScoreClamped(t *testing.T) {
	// Worst-case inputs: low base rate, penalizing payment method, huge
	// attempt/time penalties, no history bonus. Score must clamp at 0,
	// never go negative.
	opp := SyntheticOpportunity{
		FailureCategory:             domain.FailureCategoryUnknown,
		PaymentMethod:               "emi",
		PreviousAttempts:            50,
		HoursSinceFailure:           10_000,
		CustomerPriorFailedPayments: 100,
	}
	result := computeGroundTruth(opp, rand.New(rand.NewSource(1)))
	if result.TrueRecoveryProbabilityBps < 0 {
		t.Fatalf("score must clamp at 0, got %d", result.TrueRecoveryProbabilityBps)
	}
	if result.TrueRecoveryProbabilityBps != 0 {
		t.Fatalf("expected worst-case score to clamp to exactly 0, got %d", result.TrueRecoveryProbabilityBps)
	}
	if result.Recoverable {
		t.Fatal("a 0bps score must never be recoverable")
	}
}

func TestComputeGroundTruth_ScoreClampedAtMax(t *testing.T) {
	opp := SyntheticOpportunity{
		FailureCategory:                 domain.FailureCategoryTransientFailure,
		PaymentMethod:                   "upi",
		PreviousAttempts:                1,
		CustomerPriorSuccessfulPayments: 1000,
		HoursSinceFailure:               0,
	}
	result := computeGroundTruth(opp, rand.New(rand.NewSource(1)))
	if result.TrueRecoveryProbabilityBps > int(domain.MaxProbabilityBasisPoints) {
		t.Fatalf("score must clamp at %d, got %d", domain.MaxProbabilityBasisPoints, result.TrueRecoveryProbabilityBps)
	}
}

func TestComputeGroundTruth_ObservationAmbiguousIsDeterministic(t *testing.T) {
	opp := SyntheticOpportunity{FailureCategory: domain.FailureCategoryTransientFailure, PaymentMethod: "card"}
	a := computeGroundTruth(opp, rand.New(rand.NewSource(7)))
	b := computeGroundTruth(opp, rand.New(rand.NewSource(7)))
	if a.ObservationAmbiguous != b.ObservationAmbiguous {
		t.Fatal("ObservationAmbiguous is not deterministic for a fixed rand source")
	}
}

func TestComputeGroundTruth_ObservationAmbiguousIsRare(t *testing.T) {
	// Sanity check the illustrative 4% rate is roughly respected across a
	// large sample — not an exact statistical test, just a guard against
	// a sign error or unit confusion (e.g. accidentally using bps as a
	// fraction) that would make "ambiguous" the common case.
	ambiguous := 0
	const n = 10000
	for i := 0; i < n; i++ {
		r := computeGroundTruth(SyntheticOpportunity{FailureCategory: domain.FailureCategoryTransientFailure}, deriveRand(1, i, saltGroundTruth))
		if r.ObservationAmbiguous {
			ambiguous++
		}
	}
	rate := float64(ambiguous) / float64(n)
	if rate < 0.02 || rate > 0.06 {
		t.Fatalf("expected ObservationAmbiguous rate near 4%%, got %.2f%% (%d/%d)", rate*100, ambiguous, n)
	}
}

// TestGroundTruth_IndependentOfStrategy is the core anti-bias guarantee:
// the ground truth for a given opportunity must be identical regardless
// of which strategies have run, in what order, or whether they ran at
// all. Since Decide(opportunity) never receives the ground truth and
// computeGroundTruth never receives a StrategyDecision, this is
// structurally guaranteed by the function signatures — this test proves
// it holds in practice too, by generating the dataset, running every
// strategy, and then regenerating the dataset from scratch to confirm
// the ground truth didn't change.
func TestGroundTruth_IndependentOfStrategy(t *testing.T) {
	before := GenerateSyntheticDataset(2024, 100)

	for _, strat := range []EvaluationStrategy{NewFixedRetryStrategy(), NewStaticRulesStrategy(), NewRevGuardStrategy()} {
		for _, opp := range before.Opportunities {
			if _, err := strat.Decide(opp); err != nil {
				t.Fatalf("strategy %s failed: %v", strat.Name(), err)
			}
		}
	}

	after := GenerateSyntheticDataset(2024, 100)

	for i := range before.groundTruths {
		if before.groundTruths[i] != after.groundTruths[i] {
			t.Fatalf("ground truth at index %d changed after running strategies: %+v vs %+v", i, before.groundTruths[i], after.groundTruths[i])
		}
	}
}
