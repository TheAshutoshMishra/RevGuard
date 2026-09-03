package service

import (
	"context"
	"fmt"

	"revguard/backend/internal/domain"
)

// EvaluationStrategy decides, for one SyntheticOpportunity, whether and
// how to act. Its Decide method receives only the opportunity — never
// the dataset's ground truth, never another strategy's decision — so
// strategies are structurally unable to see each other's choices or the
// hidden outcome they'll later be graded against. This is what "the
// strategies must operate on equivalent opportunities" and "baselines
// cannot see RevGuard decisions" mean in code, not just in prose.
//
// A given EvaluationStrategy's Decide must be a pure function of its
// input: the same opportunity, called any number of times, must return
// the same StrategyDecision (see TestEvaluationStrategies_Deterministic).
type EvaluationStrategy interface {
	// Name is a short, stable key ("fixed_retry", "static_rules",
	// "revguard") used both as a map key in EvaluationResult and in the
	// human-readable table.
	Name() string
	Decide(opp SyntheticOpportunity) (StrategyDecision, error)
}

// StrategyDecision is the result of one strategy's decision for one
// opportunity. Outcome reuses domain.PolicyDecisionOutcome's exact
// ALLOW/BLOCK/ESCALATE vocabulary — the same three outcomes the real
// PolicyEngine produces (Milestone 5) — so that "did this strategy act,
// block, or escalate" means the same thing for a baseline as it does for
// RevGuard, rather than inventing a parallel vocabulary.
type StrategyDecision struct {
	Outcome domain.PolicyDecisionOutcome
	// Action is set only when Outcome == ALLOW: the action policy
	// authorized. This is set even when Executed is false (see below) —
	// authorization and execution are different questions, exactly as
	// they are in production (Milestone 5 authorizes, Milestone 6
	// executes, and the two can disagree).
	Action domain.RecommendedAction
	// Executed is whether the action was actually carried out. For the
	// baselines this is always true when Outcome == ALLOW (they assume
	// full capability to perform whatever they decide, since neither
	// baseline routes through RevGuard's ExecutionEngine). For
	// RevGuardStrategy this mirrors ExecutionEngine's real, current
	// limitation (Milestone 6): only retry_payment has an execution
	// implementation; every other authorized action is rejected with
	// ErrActionNotExecutable before any side effect. See
	// isRevGuardActionExecutable below.
	Executed bool
	// ActionCostMinorUnits / RiskCostMinorUnits are non-negative and set
	// only when Executed — the real cost of having taken Action, looked
	// up from the same GetActionEconomics table every strategy uses
	// (Milestone 4), so cost comparisons across strategies are
	// apples-to-apples rather than each strategy inventing its own cost
	// model. An authorized-but-unexecuted action costs nothing, exactly
	// as in production (ExecutionEngine's validation chain runs before
	// any side effect).
	ActionCostMinorUnits int64
	RiskCostMinorUnits   int64
	// ExpectedGrossRecoveryMinorUnits is the Economic Engine's ex-ante
	// prediction (Milestone 4's calculateExpectedGrossRecovery) of how
	// much would be recovered, recorded whenever Outcome == ALLOW
	// (economic evaluation runs before the execution-capability check,
	// exactly as it does in production). It is 0 for the baselines,
	// which have no economic model at all — not a claim that their
	// expectation is genuinely zero, just that the concept doesn't apply
	// to them.
	ExpectedGrossRecoveryMinorUnits int64
}

// ---------------------------------------------------------------------
// Baseline 1: Fixed Retry.
//
// Retries every qualifying failed payment up to a fixed maximum number
// of attempts. No AI diagnosis, no economic optimization, no policy
// intelligence beyond that single fixed rule. Independently implemented
// from RevGuardStrategy below: it shares only the action-economics cost
// table (the real cost of performing a retry), never the estimator,
// diagnosis, or policy rules.
// ---------------------------------------------------------------------

const fixedRetryMaxAttempts = 3

type FixedRetryStrategy struct {
	MaxAttempts int
}

func NewFixedRetryStrategy() *FixedRetryStrategy {
	return &FixedRetryStrategy{MaxAttempts: fixedRetryMaxAttempts}
}

func (s *FixedRetryStrategy) Name() string { return "fixed_retry" }

func (s *FixedRetryStrategy) Decide(opp SyntheticOpportunity) (StrategyDecision, error) {
	if opp.PreviousAttempts >= s.MaxAttempts {
		return StrategyDecision{Outcome: domain.PolicyDecisionOutcomeBlock}, nil
	}

	econ, err := GetActionEconomics(domain.RecommendedActionRetryPayment)
	if err != nil {
		return StrategyDecision{}, fmt.Errorf("fixed_retry: %w", err)
	}

	return StrategyDecision{
		Outcome:              domain.PolicyDecisionOutcomeAllow,
		Action:               domain.RecommendedActionRetryPayment,
		Executed:             true,
		ActionCostMinorUnits: econ.ActionCostMinorUnits,
		RiskCostMinorUnits:   calculateRiskCost(opp.AmountMinorUnits, econ.RiskCostBps),
	}, nil
}

// ---------------------------------------------------------------------
// Baseline 2: Static Rules.
//
// A slightly smarter, still non-adaptive baseline: retries only a fixed
// set of failure categories, only after a fixed cooldown, only under a
// fixed amount threshold, and only up to a fixed attempt count. The
// category -> action mapping is a fixed lookup table, not an AI
// recommendation. Independently implemented from RevGuardStrategy: no
// shared thresholds, no shared estimator, no shared policy rules.
// ---------------------------------------------------------------------

const (
	staticRulesCooldownHours       = 2
	staticRulesMaxAmountMinorUnits = 200_000 // INR 2,000.00
	staticRulesMaxAttempts         = 2
)

// staticRulesAllowedActions is the static, fixed category -> action
// table this baseline uses instead of an AI diagnosis.
var staticRulesAllowedActions = map[domain.FailureCategory]domain.RecommendedAction{
	domain.FailureCategoryTransientFailure:  domain.RecommendedActionRetryPayment,
	domain.FailureCategoryInsufficientFunds: domain.RecommendedActionSendPaymentLink,
}

type StaticRulesStrategy struct {
	CooldownHours       int
	MaxAmountMinorUnits int64
	MaxAttempts         int
}

func NewStaticRulesStrategy() *StaticRulesStrategy {
	return &StaticRulesStrategy{
		CooldownHours:       staticRulesCooldownHours,
		MaxAmountMinorUnits: staticRulesMaxAmountMinorUnits,
		MaxAttempts:         staticRulesMaxAttempts,
	}
}

func (s *StaticRulesStrategy) Name() string { return "static_rules" }

func (s *StaticRulesStrategy) Decide(opp SyntheticOpportunity) (StrategyDecision, error) {
	action, categoryAllowed := staticRulesAllowedActions[opp.FailureCategory]
	switch {
	case !categoryAllowed:
		return StrategyDecision{Outcome: domain.PolicyDecisionOutcomeBlock}, nil
	case opp.HoursSinceFailure < s.CooldownHours:
		return StrategyDecision{Outcome: domain.PolicyDecisionOutcomeBlock}, nil
	case opp.AmountMinorUnits > s.MaxAmountMinorUnits:
		return StrategyDecision{Outcome: domain.PolicyDecisionOutcomeBlock}, nil
	case opp.PreviousAttempts >= s.MaxAttempts:
		return StrategyDecision{Outcome: domain.PolicyDecisionOutcomeBlock}, nil
	}

	econ, err := GetActionEconomics(action)
	if err != nil {
		return StrategyDecision{}, fmt.Errorf("static_rules: %w", err)
	}

	return StrategyDecision{
		Outcome:              domain.PolicyDecisionOutcomeAllow,
		Action:               action,
		Executed:             true,
		ActionCostMinorUnits: econ.ActionCostMinorUnits,
		RiskCostMinorUnits:   calculateRiskCost(opp.AmountMinorUnits, econ.RiskCostBps),
	}, nil
}

// ---------------------------------------------------------------------
// RevGuard strategy: the actual RevGuard decision pipeline
// (diagnosis -> economic evaluation -> policy decision), reusing the
// real, unmodified components from Milestones 3-5:
//
//   - deterministicDiagnosis (evaluation_diagnosis.go) stands in for the
//     AI service call (see its doc comment for why),
//   - HeuristicProbabilityEstimator (recovery_probability_estimator.go,
//     Milestone 4) — untouched,
//   - GetActionEconomics + calculateExpectedGrossRecovery /
//     calculateRiskCost / calculateExpectedIncrementalValue
//     (action_economics.go, economic_calculations.go, Milestone 4) —
//     untouched,
//   - evaluatePolicyRules + DefaultPolicyConfig (policy_rules.go,
//     policy_config.go, Milestone 5) — untouched.
//
// No second, parallel implementation of the economic or policy logic
// exists anywhere in this file: every formula and threshold comes from
// the exact same functions the real HTTP pipeline calls.
// ---------------------------------------------------------------------

type RevGuardStrategy struct {
	estimator *HeuristicProbabilityEstimator
}

func NewRevGuardStrategy() *RevGuardStrategy {
	return &RevGuardStrategy{estimator: NewHeuristicProbabilityEstimator()}
}

func (s *RevGuardStrategy) Name() string { return "revguard" }

func (s *RevGuardStrategy) Decide(opp SyntheticOpportunity) (StrategyDecision, error) {
	diag := deterministicDiagnosis(opp)

	diagnosis := &domain.RecoveryDiagnosis{
		FailureCategory:   diag.FailureCategory,
		RecommendedAction: diag.RecommendedAction,
		Confidence:        diag.Confidence,
	}

	attempts := make([]*domain.PaymentAttempt, opp.PreviousAttempts)
	for i := range attempts {
		attempts[i] = &domain.PaymentAttempt{}
	}
	priorActions := make([]*domain.RecoveryAction, opp.PreviousRecoveryActions)
	for i := range priorActions {
		priorActions[i] = &domain.RecoveryAction{}
	}

	estimate, err := s.estimator.Estimate(context.Background(), nil, diagnosis, attempts, priorActions)
	if err != nil {
		return StrategyDecision{}, fmt.Errorf("revguard: probability estimation failed: %w", err)
	}

	econ, err := GetActionEconomics(diag.RecommendedAction)
	if err != nil {
		return StrategyDecision{}, fmt.Errorf("revguard: %w", err)
	}

	expectedGross := calculateExpectedGrossRecovery(opp.AmountMinorUnits, estimate.ProbabilityBps)
	riskCost := calculateRiskCost(opp.AmountMinorUnits, econ.RiskCostBps)
	incremental := calculateExpectedIncrementalValue(expectedGross, econ.ActionCostMinorUnits, riskCost)

	evaluation := &domain.RecoveryEconomicEvaluation{
		RecommendedAction:                  diag.RecommendedAction,
		RevenueAtRisk:                      domain.Money{MinorUnits: opp.AmountMinorUnits, Currency: opp.Currency},
		RecoveryProbabilityBps:             estimate.ProbabilityBps,
		ExpectedIncrementalValueMinorUnits: incremental,
	}

	ruleResult := evaluatePolicyRules(DefaultPolicyConfig, PolicyRuleInput{
		Diagnosis:                diagnosis,
		EconomicEvaluation:       evaluation,
		PaymentAttemptCount:      opp.PreviousAttempts,
		PriorRecoveryActionCount: opp.PreviousRecoveryActions,
	})

	decision := StrategyDecision{Outcome: ruleResult.Outcome}
	if ruleResult.Outcome == domain.PolicyDecisionOutcomeAllow {
		decision.Action = diag.RecommendedAction
		// The Economic Engine's prediction is recorded regardless of
		// execution capability, matching Milestone 4 running before
		// Milestone 6 in the real pipeline.
		decision.ExpectedGrossRecoveryMinorUnits = expectedGross
		if isRevGuardActionExecutable(diag.RecommendedAction) {
			decision.Executed = true
			decision.ActionCostMinorUnits = econ.ActionCostMinorUnits
			decision.RiskCostMinorUnits = riskCost
		}
	}
	return decision, nil
}

// isRevGuardActionExecutable mirrors ExecutionEngine.phase1's real,
// current check (backend/internal/service/execution_engine.go:
// "if decision.AuthorizedAction != domain.RecommendedActionRetryPayment
// { return nil, ErrActionNotExecutable }") — Milestone 6 only
// implemented execution for retry_payment. A policy ALLOW for any other
// action is genuinely authorized (Outcome stays ALLOW) but cannot
// actually run yet in production, so this evaluation must not credit
// RevGuard with cost or recovery it could not really have produced.
// This check applies only to RevGuardStrategy: the baselines don't route
// through RevGuard's ExecutionEngine at all, so they aren't bound by its
// current implementation coverage (see StrategyDecision.Executed's doc
// comment).
func isRevGuardActionExecutable(action domain.RecommendedAction) bool {
	return action == domain.RecommendedActionRetryPayment
}
