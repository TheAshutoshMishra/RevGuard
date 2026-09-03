package service

import (
	"fmt"
	"math/rand"

	"revguard/backend/internal/domain"
)

// This file and its evaluation_*.go siblings implement Milestone 8: a
// deterministic, offline evaluation harness that answers "does RevGuard
// recover more incremental revenue than simpler strategies?" against a
// SYNTHETIC dataset. See docs/architecture/evaluation-engine.md for the
// full design rationale.
//
// IMPORTANT: every dataset produced here is synthetic, clearly labeled as
// such (SyntheticDataset.Type / EvaluationResult.Dataset.Type == "synthetic"
// throughout), and is never real Razorpay/merchant/customer data. No
// result computed from it may be presented as a claim about live
// production performance — see the Disclaimer field on EvaluationResult.

// syntheticPaymentMethods is a small, illustrative set of payment methods
// used only to shape the synthetic dataset and the ground-truth model
// below. It is not a real Razorpay payment-method vocabulary.
var syntheticPaymentMethods = []string{"card", "upi", "netbanking", "wallet", "emi"}

// SyntheticOpportunity is one synthetic "recovery opportunity" — a failed
// payment situation a recovery strategy must decide how to act on. It
// carries only input features: no strategy, and no code path anywhere in
// this package, may see or derive the ground-truth outcome from this
// struct. That separation is what keeps the evaluation unbiased (see
// groundTruthResult in evaluation_ground_truth.go).
type SyntheticOpportunity struct {
	// ID is a synthetic, human-readable identifier (e.g. "SYN-000042"),
	// not a UUID — nothing here is persisted, so there is no need for the
	// domain's real identity scheme.
	ID string

	AmountMinorUnits int64
	Currency         domain.Currency

	FailureCategory domain.FailureCategory
	PaymentMethod   string

	CustomerPriorSuccessfulPayments int
	CustomerPriorFailedPayments     int

	// PreviousAttempts is the number of payment attempts already made on
	// the underlying payment (mirrors PolicyRuleInput.PaymentAttemptCount
	// / RecoveryProbabilityEstimator's paymentAttempts count).
	PreviousAttempts int
	// PreviousRecoveryActions is the number of recovery actions already
	// attempted on this case (mirrors PolicyRuleInput.PriorRecoveryActionCount).
	PreviousRecoveryActions int
	// HoursSinceFailure is how long ago the underlying payment failed.
	HoursSinceFailure int
}

// SyntheticDataset is a deterministically generated set of
// SyntheticOpportunity values plus their (hidden) ground-truth outcomes.
// The Type field and every consumer of this struct must keep this data
// clearly labeled SYNTHETIC — see the package doc above.
type SyntheticDataset struct {
	Seed          int64
	Type          string
	Opportunities []SyntheticOpportunity

	// groundTruths is index-aligned with Opportunities and deliberately
	// unexported: no EvaluationStrategy implementation receives it, has a
	// way to reach it through the Decide(opportunity) signature, or may
	// influence it. Only the simulation loop in evaluation_engine.go
	// reads it, strictly after every strategy has already produced its
	// decision for the corresponding opportunity.
	groundTruths []groundTruthResult
}

// datasetRandSalt distinguishes independent deterministic random streams
// derived from the same (seed, index) pair, so that generating an
// opportunity's features and computing its ground truth never share, and
// can never accidentally leak into, the same random sequence.
type datasetRandSalt int64

const (
	saltOpportunity datasetRandSalt = 1
	saltGroundTruth datasetRandSalt = 2
)

// deriveRand returns a *rand.Rand that is a pure, deterministic function
// of (seed, index, salt): the same three inputs always produce the same
// sequence of draws, and different indices/salts never collide in
// practice. This is what makes GenerateSyntheticDataset reproducible
// (same seed -> byte-identical dataset) while still keeping the
// opportunity-generation stream and the ground-truth stream independent.
func deriveRand(seed int64, index int, salt datasetRandSalt) *rand.Rand {
	mixed := seed*31 + int64(index)*1000003 + int64(salt)*7919
	return rand.New(rand.NewSource(mixed))
}

// GenerateSyntheticDataset deterministically builds count synthetic
// recovery opportunities (and their independent ground-truth outcomes)
// from seed. Calling this twice with the same (seed, count) always
// produces an identical dataset — see TestGenerateSyntheticDataset_SameSeedIsIdentical.
func GenerateSyntheticDataset(seed int64, count int) SyntheticDataset {
	opportunities := make([]SyntheticOpportunity, count)
	truths := make([]groundTruthResult, count)

	for i := 0; i < count; i++ {
		opp := generateOpportunity(seed, i)
		opportunities[i] = opp
		truths[i] = computeGroundTruth(opp, deriveRand(seed, i, saltGroundTruth))
	}

	return SyntheticDataset{
		Seed:          seed,
		Type:          "synthetic",
		Opportunities: opportunities,
		groundTruths:  truths,
	}
}

func generateOpportunity(seed int64, index int) SyntheticOpportunity {
	r := deriveRand(seed, index, saltOpportunity)

	category := domain.ValidFailureCategories[r.Intn(len(domain.ValidFailureCategories))]
	method := syntheticPaymentMethods[r.Intn(len(syntheticPaymentMethods))]

	return SyntheticOpportunity{
		ID:                              fmt.Sprintf("SYN-%06d", index),
		AmountMinorUnits:                5_000 + int64(r.Intn(495_001)), // INR 50.00 .. INR 5,000.00
		Currency:                        "INR",
		FailureCategory:                 category,
		PaymentMethod:                   method,
		CustomerPriorSuccessfulPayments: r.Intn(51),
		CustomerPriorFailedPayments:     r.Intn(11),
		PreviousAttempts:                1 + r.Intn(4), // 1..4
		PreviousRecoveryActions:         r.Intn(4),     // 0..3
		HoursSinceFailure:               r.Intn(169),   // 0..168 (up to 7 days)
	}
}
