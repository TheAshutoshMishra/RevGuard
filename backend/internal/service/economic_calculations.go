package service

import "revguard/backend/internal/domain"

// This file implements the deterministic economic formulas in isolation
// from any I/O, so they can be unit-tested directly without a database or
// any other dependency. See docs/architecture/economic-engine.md for the
// full rationale.
//
// Rounding: all division here is standard Go integer division on
// non-negative operands, which truncates toward zero — equivalent to
// floor division for non-negative values. This is applied consistently
// and is the only rounding behavior used anywhere in the Economic
// Engine: results are never adjusted after the fact, and no
// floating-point arithmetic is ever introduced.

// calculateExpectedGrossRecovery computes:
//
//	expected_gross_recovery = revenue_at_risk * probability_bps / 10000
//
// Both inputs are non-negative, so the result is always non-negative.
func calculateExpectedGrossRecovery(revenueAtRiskMinorUnits int64, probabilityBps domain.ProbabilityBasisPoints) int64 {
	return revenueAtRiskMinorUnits * int64(probabilityBps) / int64(domain.MaxProbabilityBasisPoints)
}

// calculateRiskCost computes:
//
//	risk_cost = revenue_at_risk * risk_cost_bps / 10000
//
// using the same basis-points-of-revenue convention as recovery
// probability, so both scale consistently with the size of the payment
// at risk.
func calculateRiskCost(revenueAtRiskMinorUnits int64, riskCostBps int32) int64 {
	return revenueAtRiskMinorUnits * int64(riskCostBps) / int64(domain.MaxProbabilityBasisPoints)
}

// calculateExpectedIncrementalValue computes:
//
//	expected_incremental_value = expected_gross_recovery - action_cost - risk_cost
//
// This is deliberately signed: a negative result (costs exceed expected
// gross recovery) is a valid, meaningful outcome — it means the
// recommended action does not have positive expected economic value. The
// Economic Engine records this either way; deciding what to do about a
// negative value is a policy decision for a later milestone.
func calculateExpectedIncrementalValue(expectedGrossRecoveryMinorUnits, actionCostMinorUnits, riskCostMinorUnits int64) int64 {
	return expectedGrossRecoveryMinorUnits - actionCostMinorUnits - riskCostMinorUnits
}
