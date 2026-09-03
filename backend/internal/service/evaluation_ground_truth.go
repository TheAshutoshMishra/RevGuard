package service

import (
	"math/rand"

	"revguard/backend/internal/domain"
)

// groundTruthResult is the hidden, deterministic "what would actually
// happen" outcome for one SyntheticOpportunity. It is computed once, at
// dataset-generation time, from the opportunity's own features plus an
// independent random draw — never from any strategy's decision. No
// EvaluationStrategy implementation ever receives a groundTruthResult;
// only evaluation_engine.go's simulation loop reads it, after every
// strategy has already produced its decision. That ordering, plus the
// unexported field on SyntheticDataset, is what guarantees the ground
// truth cannot be biased toward (or against) any particular strategy —
// see the "fairness / anti-bias" tests in evaluation_fairness_test.go.
type groundTruthResult struct {
	// Recoverable is whether an appropriate, automated recovery action
	// taken on this opportunity would actually recover the money.
	Recoverable bool
	// TrueRecoveryProbabilityBps is recorded for diagnostic/debugging
	// purposes only (e.g. dataset inspection tooling); no strategy or
	// metric calculation reads it.
	TrueRecoveryProbabilityBps int
	// ObservationAmbiguous models Milestone 6/7's real, documented
	// possibility that even a genuinely executed action never produces a
	// definitive financial-truth signal within the observation window —
	// a provider timeout at execution time
	// (RecoveryActionStatusUnknown / PROVIDER_RESPONSE_AMBIGUOUS) or an
	// unresolved reconciliation lookup
	// (ErrNoProviderReferenceToReconcile / ErrReconciliationReferenceNotFound).
	// It is independent of Recoverable: whether the money was genuinely
	// recoverable is a separate question from whether RevGuard would
	// ever find out. Milestone 7 never guesses an UNKNOWN case into
	// SUCCESS or FAILED, and neither does this evaluation — see
	// resolveFinancialOutcome in evaluation_metrics.go.
	ObservationAmbiguous bool
}

// groundTruthAmbiguousRateBps is an illustrative, documented ASSUMPTION
// about how often a genuinely executed action's financial outcome would
// never resolve within the observation window (provider timeout,
// unresolved reconciliation) — NOT a measured Razorpay reliability
// figure. Milestone 7 itself has no automatic background reconciliation
// (see docs/architecture/webhooks-reconciliation.md's "Known
// limitations"), so a nonzero rate of permanently-UNKNOWN outcomes is a
// real, honest property of the current system to model here, not an
// artifact invented for this evaluation.
const groundTruthAmbiguousRateBps = 400 // 4%

// groundTruthBaseRateBps are the ground-truth model's own illustrative
// assumptions about how recoverable each failure category actually is.
// These are DELIBERATELY DEFINED INDEPENDENTLY of
// heuristicBaseRateBps (RevGuard's own probability estimator, Milestone 4):
// reusing the same table here would let RevGuard's evaluation of its own
// recovery odds always exactly match reality, which would artificially
// favor RevGuard's decisions over the baselines' in the evaluation. Using
// a separate, differently-tuned table means RevGuard's estimator is
// itself imperfect against ground truth, same as it would be against
// real-world outcomes it can't observe in advance.
//
// These are illustrative, NOT measured Razorpay data, NOT derived from
// any historical recovery outcomes.
var groundTruthBaseRateBps = map[domain.FailureCategory]int{
	domain.FailureCategoryTransientFailure:    6500,
	domain.FailureCategoryInsufficientFunds:   4000,
	domain.FailureCategoryPaymentMethodIssue:  3500,
	domain.FailureCategoryAuthenticationIssue: 5000,
	domain.FailureCategoryMandateIssue:        2000,
	domain.FailureCategoryCustomerAbandonment: 2500,
	domain.FailureCategoryUnknown:             1500,
}

// groundTruthPaymentMethodModifierBps is an illustrative adjustment for
// how the customer's payment method affects real recovery odds (e.g. a
// UPI retry is assumed to succeed more often than a netbanking one).
var groundTruthPaymentMethodModifierBps = map[string]int{
	"card":       0,
	"upi":        500,
	"netbanking": -300,
	"wallet":     200,
	"emi":        -500,
}

const (
	// groundTruthMaxHistoryBonusBps caps how much a customer's prior
	// successful-payment history can raise their recovery odds.
	groundTruthMaxHistoryBonusBps    = 1000
	groundTruthHistoryBonusPerPay    = 20
	groundTruthHistoryPenaltyPerFail = 50
	// groundTruthAttemptPenaltyBps is subtracted per payment attempt
	// beyond the first: a payment that has already failed repeatedly is
	// assumed less likely to be genuinely recoverable.
	groundTruthAttemptPenaltyBps = 400
	// groundTruthHourlyDecayBps is subtracted per full day since the
	// failure occurred: recovery odds are assumed to fade over time.
	groundTruthHourlyDecayBps = 100
)

// computeGroundTruth is a pure function of (opp, r): given the same
// opportunity and the same *rand.Rand draw sequence, it always returns
// the same result. It never reads any strategy's decision and is called
// exactly once per opportunity, before any strategy runs.
func computeGroundTruth(opp SyntheticOpportunity, r *rand.Rand) groundTruthResult {
	base := groundTruthBaseRateBps[opp.FailureCategory]

	score := base + groundTruthPaymentMethodModifierBps[opp.PaymentMethod]

	historyBonus := opp.CustomerPriorSuccessfulPayments * groundTruthHistoryBonusPerPay
	if historyBonus > groundTruthMaxHistoryBonusBps {
		historyBonus = groundTruthMaxHistoryBonusBps
	}
	score += historyBonus
	score -= opp.CustomerPriorFailedPayments * groundTruthHistoryPenaltyPerFail

	if opp.PreviousAttempts > 1 {
		score -= (opp.PreviousAttempts - 1) * groundTruthAttemptPenaltyBps
	}
	score -= (opp.HoursSinceFailure / 24) * groundTruthHourlyDecayBps

	if score < 0 {
		score = 0
	}
	if score > int(domain.MaxProbabilityBasisPoints) {
		score = int(domain.MaxProbabilityBasisPoints)
	}

	// Both draws come from the same per-opportunity *rand.Rand stream
	// (saltGroundTruth) in a fixed order, so the result stays a pure,
	// deterministic function of (seed, index) — see deriveRand.
	recoverableDraw := r.Intn(int(domain.MaxProbabilityBasisPoints))
	ambiguousDraw := r.Intn(int(domain.MaxProbabilityBasisPoints))

	return groundTruthResult{
		Recoverable:                recoverableDraw < score,
		TrueRecoveryProbabilityBps: score,
		ObservationAmbiguous:       ambiguousDraw < groundTruthAmbiguousRateBps,
	}
}
