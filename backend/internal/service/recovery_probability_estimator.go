package service

import (
	"context"
	"fmt"

	"revguard/backend/internal/domain"
)

// ProbabilityEstimate is the deterministic output of a
// RecoveryProbabilityEstimator.
type ProbabilityEstimate struct {
	ProbabilityBps   domain.ProbabilityBasisPoints
	EstimatorName    string
	EstimatorVersion string
	Explanation      string
}

// RecoveryProbabilityEstimator estimates the probability that a
// RecoveryCase's revenue will actually be recovered if the diagnosed
// recommendation is acted on.
//
// This is explicitly NOT the AI's confidence in its own recommendation
// (domain.RecoveryDiagnosis.Confidence). AI confidence answers "how sure
// is the model in this recommendation"; recovery probability answers "how
// likely is the money to actually come back." These are different
// questions and this codebase never substitutes one for the other — see
// docs/architecture/economic-engine.md.
type RecoveryProbabilityEstimator interface {
	Estimate(
		ctx context.Context,
		recoveryCase *domain.RecoveryCase,
		diagnosis *domain.RecoveryDiagnosis,
		paymentAttempts []*domain.PaymentAttempt,
		priorRecoveryActions []*domain.RecoveryAction,
	) (ProbabilityEstimate, error)
}

// HeuristicEstimatorVersion identifies HeuristicProbabilityEstimator's
// current rule set. Bump it whenever the base rates, multipliers, or
// penalties below change.
const HeuristicEstimatorVersion = "heuristic-v1"

const heuristicEstimatorName = "heuristic"

// heuristicBaseRateBps are illustrative, documented ASSUMPTIONS about
// how recoverable each failure category typically is — NOT measured
// production benchmarks, NOT derived from any historical RevGuard or
// Razorpay data, and NOT a machine-learned model. They exist to make the
// Economic Engine's output deterministic and explainable while Milestone
// 4 has no real historical calibration data available. See
// docs/architecture/economic-engine.md for the rationale and the plan to
// replace this with calibrated data in a future milestone.
var heuristicBaseRateBps = map[domain.FailureCategory]domain.ProbabilityBasisPoints{
	domain.FailureCategoryTransientFailure:    6000, // often self-resolves on retry
	domain.FailureCategoryInsufficientFunds:   3500,
	domain.FailureCategoryPaymentMethodIssue:  4000,
	domain.FailureCategoryAuthenticationIssue: 4500,
	domain.FailureCategoryMandateIssue:        2500, // typically needs bank/human involvement
	domain.FailureCategoryCustomerAbandonment: 3000,
	domain.FailureCategoryUnknown:             2000, // low-confidence baseline
}

// heuristicActionMultiplierPercent adjusts the base rate up or down
// depending on how effective the recommended action is generally assumed
// to be at converting that opportunity into recovered revenue. Also
// illustrative, not measured.
var heuristicActionMultiplierPercent = map[domain.RecommendedAction]int64{
	domain.RecommendedActionRetryPayment:               100,
	domain.RecommendedActionSendPaymentLink:            110,
	domain.RecommendedActionRequestPaymentMethodChange: 90,
	domain.RecommendedActionSendReminder:               80,
	domain.RecommendedActionEscalateToHuman:            70,
	domain.RecommendedActionStopRecovery:               0, // recommending to stop implies no further recovery is expected
}

const (
	// heuristicAttemptPenaltyBps is subtracted per payment attempt
	// beyond the first: each additional failed attempt is assumed to
	// indicate a harder-to-recover case (diminishing returns).
	heuristicAttemptPenaltyBps = 500
	// heuristicPriorActionPenaltyBps is subtracted per prior recovery
	// action already attempted on this case: repeated recovery attempts
	// on the same case are assumed to mean earlier attempts already
	// failed to resolve it.
	heuristicPriorActionPenaltyBps = 800
)

// HeuristicProbabilityEstimator is a transparent, rule-based
// RecoveryProbabilityEstimator. It is explicitly NOT machine learning and
// does NOT call the AI service — see the package-level documentation on
// RecoveryProbabilityEstimator. Its output is a pure, deterministic
// function of its inputs: the same case/diagnosis/attempts/actions always
// produce the same ProbabilityEstimate.
type HeuristicProbabilityEstimator struct{}

func NewHeuristicProbabilityEstimator() *HeuristicProbabilityEstimator {
	return &HeuristicProbabilityEstimator{}
}

func (e *HeuristicProbabilityEstimator) Estimate(
	_ context.Context,
	_ *domain.RecoveryCase,
	diagnosis *domain.RecoveryDiagnosis,
	paymentAttempts []*domain.PaymentAttempt,
	priorRecoveryActions []*domain.RecoveryAction,
) (ProbabilityEstimate, error) {
	baseBps, ok := heuristicBaseRateBps[diagnosis.FailureCategory]
	if !ok {
		return ProbabilityEstimate{}, fmt.Errorf("%w: failure_category %q", domain.ErrInvalidProbability, diagnosis.FailureCategory)
	}
	multiplier, ok := heuristicActionMultiplierPercent[diagnosis.RecommendedAction]
	if !ok {
		return ProbabilityEstimate{}, fmt.Errorf("%w: %q", ErrUnknownRecommendedAction, diagnosis.RecommendedAction)
	}

	adjusted := int64(baseBps) * multiplier / 100

	attemptPenalty := int64(0)
	if n := len(paymentAttempts); n > 1 {
		attemptPenalty = int64(n-1) * heuristicAttemptPenaltyBps
	}
	actionPenalty := int64(len(priorRecoveryActions)) * heuristicPriorActionPenaltyBps

	result := adjusted - attemptPenalty - actionPenalty
	if result < 0 {
		result = 0
	}
	if result > int64(domain.MaxProbabilityBasisPoints) {
		result = int64(domain.MaxProbabilityBasisPoints)
	}

	probabilityBps, err := domain.NewProbabilityBasisPoints(int(result))
	if err != nil {
		// Unreachable given the clamp above; kept for defense in depth.
		return ProbabilityEstimate{}, err
	}

	return ProbabilityEstimate{
		ProbabilityBps:   probabilityBps,
		EstimatorName:    heuristicEstimatorName,
		EstimatorVersion: HeuristicEstimatorVersion,
		Explanation: fmt.Sprintf(
			"base=%dbps(%s) x multiplier=%d%%(%s) - attempt_penalty=%dbps(%d attempts) - prior_action_penalty=%dbps(%d actions) = %dbps",
			int(baseBps), diagnosis.FailureCategory, multiplier, diagnosis.RecommendedAction,
			attemptPenalty, len(paymentAttempts), actionPenalty, len(priorRecoveryActions), int(probabilityBps),
		),
	}, nil
}
