package service

import (
	"fmt"

	"revguard/backend/internal/domain"
)

// EconomicModelVersion identifies this action-economics table together
// with the formulas in economic_calculations.go. Bump it whenever either
// changes in a way that would make past evaluations non-reproducible
// under the new logic.
const EconomicModelVersion = "economic-model-v1"

// ActionEconomics describes the cost/risk assumptions RevGuard's
// Economic Engine uses for a given RecommendedAction.
//
// These are illustrative demonstration defaults for RevGuard, NOT
// measured Razorpay costs, NOT a real production cost model, and NOT
// derived from any historical data. See
// docs/architecture/economic-engine.md for the full rationale and known
// limitations. A future milestone may replace this fixed table with
// configurable or historically-calibrated values without changing the
// Economic Engine's interface.
type ActionEconomics struct {
	// ActionCostMinorUnits is a fixed operational cost assumed for
	// attempting the action (e.g. a gateway retry fee, an SMS/email
	// send cost, human agent time), in the evaluation's currency minor
	// units. Always non-negative.
	ActionCostMinorUnits int64
	// RiskCostBps expresses the action's assumed downside risk as a
	// proportion of revenue at risk, in basis points (see
	// domain.ProbabilityBasisPoints — reused here for the same
	// integer-arithmetic determinism, even though this is a cost rate
	// rather than a probability). Always non-negative.
	RiskCostBps int32
}

// defaultActionEconomics is RevGuard-v1's illustrative cost/risk table.
// Every domain.RecommendedAction value must have an entry — an action
// missing here is treated as a configuration error (GetActionEconomics
// returns ErrUnknownRecommendedAction), not silently defaulted.
var defaultActionEconomics = map[domain.RecommendedAction]ActionEconomics{
	// A gateway retry has a small assumed processing cost and low risk
	// (it's the same payment method the customer already tried).
	domain.RecommendedActionRetryPayment: {ActionCostMinorUnits: 500, RiskCostBps: 50},
	// Sending a payment link has a low fixed cost (messaging) and low
	// risk.
	domain.RecommendedActionSendPaymentLink: {ActionCostMinorUnits: 200, RiskCostBps: 30},
	// Asking the customer to change payment method costs a bit more to
	// facilitate and carries slightly higher risk of an incomplete flow.
	domain.RecommendedActionRequestPaymentMethodChange: {ActionCostMinorUnits: 300, RiskCostBps: 40},
	// A reminder is cheap and low-risk.
	domain.RecommendedActionSendReminder: {ActionCostMinorUnits: 50, RiskCostBps: 10},
	// Human escalation is assumed to be the most expensive action
	// (agent time) but low incremental risk beyond that cost.
	domain.RecommendedActionEscalateToHuman: {ActionCostMinorUnits: 5000, RiskCostBps: 20},
	// Stopping recovery has no cost and no risk — nothing is attempted.
	domain.RecommendedActionStopRecovery: {ActionCostMinorUnits: 0, RiskCostBps: 0},
}

// ErrUnknownRecommendedAction means a domain.RecommendedAction has no
// corresponding ActionEconomics entry (or, more generally, is not one of
// the six values RecommendedAction.Valid() accepts).
var ErrUnknownRecommendedAction = fmt.Errorf("service: unknown recommended action")

// GetActionEconomics returns the cost/risk assumptions for action, or
// ErrUnknownRecommendedAction if action is not a recognized value.
func GetActionEconomics(action domain.RecommendedAction) (ActionEconomics, error) {
	economics, ok := defaultActionEconomics[action]
	if !ok {
		return ActionEconomics{}, fmt.Errorf("%w: %q", ErrUnknownRecommendedAction, action)
	}
	return economics, nil
}
