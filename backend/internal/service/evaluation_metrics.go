package service

import "revguard/backend/internal/domain"

// StrategyMetrics is every metric M8/M9 require for one strategy, computed
// purely from that strategy's own decisions plus the dataset's
// (strategy-independent) ground truth. See RunEvaluation in
// evaluation_engine.go for exactly how each field is produced, and
// docs/architecture/evaluation-engine.md for the formulas in prose.
type StrategyMetrics struct {
	Name string `json:"name"`

	// RevenueRecoveredMinorUnits = sum, over every opportunity where this
	// strategy actually executed an action (Executed == true) AND the
	// simulated financial outcome (resolveFinancialOutcome) is SUCCESS,
	// of the opportunity's full amount. Never counts an executed action
	// whose outcome is FAILED or UNKNOWN — money is only ever recovered
	// on a definitive SUCCESS, matching Milestone 7's financial-truth
	// rule that an outcome is never fabricated.
	RevenueRecoveredMinorUnits int64 `json:"revenue_recovered_minor_units"`

	// RecoveryRate = RevenueRecoveredMinorUnits / dataset RevenueAtRisk.
	// A ratio in [0, 1], 0 when RevenueAtRisk is 0. This is a display
	// ratio, not a monetary value, so float64 is used deliberately (same
	// convention as domain.RecoveryDiagnosis.Confidence).
	RecoveryRate float64 `json:"recovery_rate"`

	// RecoveryCostMinorUnits / RiskCostMinorUnits = sum of
	// StrategyDecision.ActionCostMinorUnits / RiskCostMinorUnits across
	// every opportunity. Both are 0 for an opportunity where Executed is
	// false (blocked, escalated, or authorized-but-unsupported).
	RecoveryCostMinorUnits int64 `json:"recovery_cost_minor_units"`
	RiskCostMinorUnits     int64 `json:"risk_cost_minor_units"`

	// ExpectedRecoveryValueMinorUnits = sum of
	// StrategyDecision.ExpectedGrossRecoveryMinorUnits across every
	// ALLOW decision (executed or not) — RevGuard's Economic Engine's
	// ex-ante prediction of recovered revenue, before the (separately
	// modeled) ground truth is applied. Always 0 for the baselines,
	// which have no economic model. Comparing this to
	// RevenueRecoveredMinorUnits is a calibration check on the estimator
	// (see docs/architecture/evaluation-engine.md), not a claim that the
	// estimator should exactly match a single simulation run.
	ExpectedRecoveryValueMinorUnits int64 `json:"expected_recovery_value_minor_units"`

	// NetIncrementalValueMinorUnits = RevenueRecoveredMinorUnits -
	// RecoveryCostMinorUnits - RiskCostMinorUnits. Signed: a strategy
	// that spends more on actions than it recovers has a negative net
	// value.
	NetIncrementalValueMinorUnits int64 `json:"net_incremental_value_minor_units"`

	// ActionsTaken counts opportunities where the strategy actually
	// executed an action (Executed == true) — not merely authorized one.
	ActionsTaken int `json:"actions_taken"`
	// ActionsBlocked / HumanEscalations count
	// StrategyDecision.Outcome == BLOCK / ESCALATE respectively.
	ActionsBlocked   int `json:"actions_blocked"`
	HumanEscalations int `json:"human_escalations"`

	// UnsupportedActions counts opportunities where policy authorized an
	// action (Outcome == ALLOW) that could not actually be executed
	// (Executed == false) — Milestone 6's real, current scope limitation
	// (only retry_payment is implemented). Always 0 for the baselines,
	// which assume full execution capability for whatever they decide.
	UnsupportedActions int `json:"unsupported_actions"`

	// AmbiguousOutcomes counts opportunities where an action was
	// executed but the simulated financial-truth signal never resolved
	// (mirrors Milestone 6/7's UNKNOWN — provider timeout or unresolved
	// reconciliation). Never counted as recovered, never counted as
	// UnnecessaryActions (an unresolved case is not proven wasted,
	// unlike a definitively FAILED one).
	AmbiguousOutcomes int `json:"ambiguous_outcomes"`

	// UnnecessaryActions counts every executed action whose outcome
	// definitively FAILED (not UNKNOWN) — cost was spent and nothing was
	// recovered, and RevGuard/the baseline could not have known better
	// from the ground truth's perspective.
	UnnecessaryActions int `json:"unnecessary_actions"`

	// AverageAttempts is the mean of SyntheticOpportunity.PreviousAttempts
	// across opportunities where the strategy executed an action. 0 when
	// ActionsTaken is 0. This measures how deep into a payment's retry
	// history a strategy is still willing to act.
	AverageAttempts float64 `json:"average_attempts"`

	Currency string `json:"currency"`
}

// resolveFinancialOutcome is the evaluation's stand-in for Milestone
// 6/7's real execution + webhook/reconciliation pipeline: given that an
// action was genuinely executed, it decides whether the simulated
// financial truth is SUCCESS, FAILED, or UNKNOWN, reusing
// domain.RecoveryOutcomeStatus's exact vocabulary (Milestone 1/7) rather
// than inventing a parallel one. It must only be called when the
// decision was actually executed — an authorized-but-unsupported or
// blocked/escalated decision has no financial outcome at all (there is
// nothing to resolve).
//
// Ordering matters and mirrors production: Milestone 7 never guesses an
// ambiguous (UNKNOWN) case into SUCCESS or FAILED, so ObservationAmbiguous
// is checked first, independent of whether the money was genuinely
// recoverable.
func resolveFinancialOutcome(truth groundTruthResult) domain.RecoveryOutcomeStatus {
	if truth.ObservationAmbiguous {
		return domain.RecoveryOutcomeStatusUnknown
	}
	if truth.Recoverable {
		return domain.RecoveryOutcomeStatusSuccess
	}
	return domain.RecoveryOutcomeStatusFailed
}

// aggregateStrategyMetrics computes StrategyMetrics for one strategy's
// decisions. decisions and dataset.Opportunities/groundTruths must be
// the same length and index-aligned (guaranteed by RunEvaluation, which
// is the only caller). Every opportunity contributes to exactly one of
// ActionsBlocked / HumanEscalations / UnsupportedActions / (SUCCESS ->
// RevenueRecoveredMinorUnits) / (FAILED -> UnnecessaryActions) /
// (UNKNOWN -> AmbiguousOutcomes) — never more than one, so nothing is
// double-counted (see TestAggregateStrategyMetrics_NoDoubleCounting).
func aggregateStrategyMetrics(name string, dataset SyntheticDataset, decisions []StrategyDecision, revenueAtRiskMinorUnits int64) StrategyMetrics {
	m := StrategyMetrics{Name: name, Currency: "INR"}

	var attemptsSumForActedOpportunities int64

	for i, decision := range decisions {
		opp := dataset.Opportunities[i]
		truth := dataset.groundTruths[i]

		switch decision.Outcome {
		case domain.PolicyDecisionOutcomeBlock:
			m.ActionsBlocked++
			continue
		case domain.PolicyDecisionOutcomeEscalate:
			m.HumanEscalations++
			continue
		case domain.PolicyDecisionOutcomeAllow:
			// falls through below
		default:
			continue
		}

		m.ExpectedRecoveryValueMinorUnits += decision.ExpectedGrossRecoveryMinorUnits

		if !decision.Executed {
			m.UnsupportedActions++
			continue
		}

		m.ActionsTaken++
		m.RecoveryCostMinorUnits += decision.ActionCostMinorUnits
		m.RiskCostMinorUnits += decision.RiskCostMinorUnits
		attemptsSumForActedOpportunities += int64(opp.PreviousAttempts)

		switch resolveFinancialOutcome(truth) {
		case domain.RecoveryOutcomeStatusSuccess:
			m.RevenueRecoveredMinorUnits += opp.AmountMinorUnits
		case domain.RecoveryOutcomeStatusFailed:
			m.UnnecessaryActions++
		case domain.RecoveryOutcomeStatusUnknown:
			m.AmbiguousOutcomes++
		}
	}

	if revenueAtRiskMinorUnits > 0 {
		m.RecoveryRate = float64(m.RevenueRecoveredMinorUnits) / float64(revenueAtRiskMinorUnits)
	}
	if m.ActionsTaken > 0 {
		m.AverageAttempts = float64(attemptsSumForActedOpportunities) / float64(m.ActionsTaken)
	}
	m.NetIncrementalValueMinorUnits = m.RevenueRecoveredMinorUnits - m.RecoveryCostMinorUnits - m.RiskCostMinorUnits

	return m
}
