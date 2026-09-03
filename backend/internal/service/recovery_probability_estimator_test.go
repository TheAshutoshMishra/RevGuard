package service_test

import (
	"context"
	"testing"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/service"
)

func diagnosisWith(category domain.FailureCategory, action domain.RecommendedAction) *domain.RecoveryDiagnosis {
	return &domain.RecoveryDiagnosis{
		FailureCategory:   category,
		RecommendedAction: action,
	}
}

func TestHeuristicEstimator_AllFailureCategories(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	for _, category := range domain.ValidFailureCategories {
		t.Run(string(category), func(t *testing.T) {
			diagnosis := diagnosisWith(category, domain.RecommendedActionRetryPayment)
			estimate, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, nil, nil)
			if err != nil {
				t.Fatalf("Estimate: %v", err)
			}
			if estimate.ProbabilityBps < 0 || estimate.ProbabilityBps > domain.MaxProbabilityBasisPoints {
				t.Fatalf("probability out of range: %d", estimate.ProbabilityBps)
			}
			if estimate.EstimatorName != "heuristic" {
				t.Errorf("expected estimator_name=heuristic, got %q", estimate.EstimatorName)
			}
			if estimate.EstimatorVersion != service.HeuristicEstimatorVersion {
				t.Errorf("expected estimator_version=%s, got %q", service.HeuristicEstimatorVersion, estimate.EstimatorVersion)
			}
			if estimate.Explanation == "" {
				t.Error("expected a non-empty explanation")
			}
		})
	}
}

func TestHeuristicEstimator_AllRecommendedActions(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	for _, action := range domain.ValidRecommendedActions {
		t.Run(string(action), func(t *testing.T) {
			diagnosis := diagnosisWith(domain.FailureCategoryTransientFailure, action)
			estimate, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, nil, nil)
			if err != nil {
				t.Fatalf("Estimate: %v", err)
			}
			if estimate.ProbabilityBps < 0 || estimate.ProbabilityBps > domain.MaxProbabilityBasisPoints {
				t.Fatalf("probability out of range: %d", estimate.ProbabilityBps)
			}
		})
	}
}

func TestHeuristicEstimator_StopRecoveryAlwaysZero(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	for _, category := range domain.ValidFailureCategories {
		diagnosis := diagnosisWith(category, domain.RecommendedActionStopRecovery)
		estimate, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, nil, nil)
		if err != nil {
			t.Fatalf("Estimate: %v", err)
		}
		if estimate.ProbabilityBps != 0 {
			t.Errorf("category %s: expected 0 bps for stop_recovery, got %d", category, estimate.ProbabilityBps)
		}
	}
}

func TestHeuristicEstimator_Deterministic(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	diagnosis := diagnosisWith(domain.FailureCategoryInsufficientFunds, domain.RecommendedActionSendPaymentLink)
	attempts := []*domain.PaymentAttempt{{AttemptNumber: 1}, {AttemptNumber: 2}}
	actions := []*domain.RecoveryAction{{AttemptNumber: 1}}

	first, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, attempts, actions)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	second, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, attempts, actions)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}

	if first.ProbabilityBps != second.ProbabilityBps {
		t.Fatalf("non-deterministic: got %d then %d for identical inputs", first.ProbabilityBps, second.ProbabilityBps)
	}
}

func TestHeuristicEstimator_MoreAttemptsAndActionsLowerProbability(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	diagnosis := diagnosisWith(domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)

	baseline, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, nil, nil)
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}

	withHistory, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis,
		[]*domain.PaymentAttempt{{AttemptNumber: 1}, {AttemptNumber: 2}, {AttemptNumber: 3}},
		[]*domain.RecoveryAction{{AttemptNumber: 1}, {AttemptNumber: 2}})
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}

	if withHistory.ProbabilityBps >= baseline.ProbabilityBps {
		t.Fatalf("expected probability to decrease with more attempts/actions: baseline=%d withHistory=%d",
			baseline.ProbabilityBps, withHistory.ProbabilityBps)
	}
}

func TestHeuristicEstimator_UnknownFailureCategoryRejected(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	diagnosis := diagnosisWith(domain.FailureCategory("bogus"), domain.RecommendedActionRetryPayment)
	_, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, nil, nil)
	if err == nil {
		t.Fatal("expected an error for unknown failure category")
	}
}

func TestHeuristicEstimator_UnknownActionRejected(t *testing.T) {
	estimator := service.NewHeuristicProbabilityEstimator()
	diagnosis := diagnosisWith(domain.FailureCategoryTransientFailure, domain.RecommendedAction("bogus"))
	_, err := estimator.Estimate(context.Background(), &domain.RecoveryCase{}, diagnosis, nil, nil)
	if err == nil {
		t.Fatal("expected an error for unknown recommended action")
	}
}
