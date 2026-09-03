package service

import (
	"fmt"
	"strings"
	"time"
)

// evaluationDisclaimer is carried on every EvaluationResult so that no
// consumer of this output (CLI, JSON file, dashboard built on top of it
// later) can present it without also seeing this label. Required by
// this milestone's non-negotiable rules 5, 7, 8, 9.
const evaluationDisclaimer = "This evaluation runs entirely against a SYNTHETIC, deterministically generated dataset (docs/architecture/evaluation-engine.md), and every strategy below — the two baselines and all three RevGuard policy profiles — is evaluated against that exact same dataset. It has NOT been run against, or validated against, live Razorpay production data, and its results must never be presented as a claim about real-world RevGuard performance."

// DatasetSummary describes the dataset an EvaluationResult was computed
// from. Type is always "synthetic" in this milestone — there is no code
// path that produces any other value.
type DatasetSummary struct {
	Seed          int64  `json:"seed"`
	Opportunities int    `json:"opportunities"`
	Type          string `json:"type"`

	// RevenueAtRiskMinorUnits = sum of every opportunity's amount. Same
	// for every strategy, since all strategies see the same dataset.
	RevenueAtRiskMinorUnits int64 `json:"revenue_at_risk_minor_units"`

	// PotentiallyRecoverableRevenueMinorUnits = sum of every
	// opportunity's amount where the (strategy-independent) ground-truth
	// model says recovery was actually possible. This is the ceiling no
	// strategy can exceed, no matter how it acts.
	PotentiallyRecoverableRevenueMinorUnits int64 `json:"potentially_recoverable_revenue_minor_units"`

	Currency string `json:"currency"`
}

// ComparisonResult is one RevGuard policy profile measured against one
// baseline strategy, both run against the identical dataset.
type ComparisonResult struct {
	// ProfileName identifies which RevGuard profile this comparison is
	// for (Milestone 10: "revguard_conservative", "revguard_balanced",
	// or "revguard_aggressive").
	ProfileName  string `json:"profile_name"`
	BaselineName string `json:"baseline_name"`

	// IncrementalRecoveredRevenueMinorUnits =
	//   profile.RevenueRecoveredMinorUnits - baseline.RevenueRecoveredMinorUnits
	IncrementalRecoveredRevenueMinorUnits int64 `json:"incremental_recovered_revenue_minor_units"`

	// IncrementalNetValueMinorUnits =
	//   profile.NetIncrementalValueMinorUnits - baseline.NetIncrementalValueMinorUnits
	IncrementalNetValueMinorUnits int64 `json:"incremental_net_value_minor_units"`

	// ActionReductionPercent =
	//   (baseline.ActionsTaken - profile.ActionsTaken) / baseline.ActionsTaken * 100
	// A positive value means the profile took fewer actions than the
	// baseline; negative means more. 0 when the baseline took no
	// actions. A display ratio, not money, hence float64.
	ActionReductionPercent float64 `json:"action_reduction_percent"`

	// IncrementalRecoveryRate = profile.RecoveryRate - baseline.RecoveryRate.
	// A ratio-of-ratios, not money: can be negative, is 0 when both
	// rates are equal, and is independent of
	// IncrementalRecoveredRevenueMinorUnits (a strategy can recover a
	// smaller absolute amount but still improve the rate if it also
	// took far fewer, more selective actions — see
	// docs/architecture/evaluation-engine.md).
	IncrementalRecoveryRate float64 `json:"incremental_recovery_rate"`
}

// EvaluationResult is the complete, machine-readable output of one
// evaluation run. It is a pure function of (seed, cases) — running
// RunEvaluation twice with the same arguments produces a value that
// marshals to byte-identical JSON (see
// TestRunEvaluation_Reproducible).
type EvaluationResult struct {
	Dataset     DatasetSummary              `json:"dataset"`
	Strategies  map[string]StrategyMetrics  `json:"strategies"`
	Comparisons map[string]ComparisonResult `json:"comparisons"`
	Disclaimer  string                      `json:"disclaimer"`
}

// baselineStrategyNames are the two non-RevGuard strategies every
// RevGuard profile is compared against.
var baselineStrategyNames = []string{"fixed_retry", "static_rules"}

// revGuardProfileKeys pairs each Milestone 10 policy profile with the
// strategy-name key it appears under in EvaluationResult.Strategies —
// e.g. "revguard_conservative" uses ConservativePolicyConfig. Order here
// is also the fixed display order for FormatResultTable/FormatMarkdownReport.
var revGuardProfileKeys = []struct {
	Key    string
	Config PolicyConfig
}{
	{"revguard_conservative", ConservativePolicyConfig},
	{"revguard_balanced", BalancedPolicyConfig},
	{"revguard_aggressive", AggressivePolicyConfig},
}

// RunEvaluation is RevGuard's deterministic, offline evaluation
// pipeline: generate a synthetic dataset, run the two baselines and all
// three RevGuard policy profiles over the exact same dataset, apply the
// shared (strategy-independent) ground truth, aggregate metrics, and
// compare each RevGuard profile against each baseline. It performs no
// I/O, no network calls, and touches no database — every number in the
// result is calculated here from the generated data, never hard-coded,
// and the dataset itself is never altered based on how any strategy
// performs (see docs/architecture/evaluation-engine.md's fairness
// guarantees).
func RunEvaluation(seed int64, cases int) (*EvaluationResult, error) {
	if cases < 0 {
		return nil, fmt.Errorf("service: cases must be >= 0, got %d", cases)
	}

	dataset := GenerateSyntheticDataset(seed, cases)

	var revenueAtRisk, potentiallyRecoverable int64
	for i, opp := range dataset.Opportunities {
		revenueAtRisk += opp.AmountMinorUnits
		if dataset.groundTruths[i].Recoverable {
			potentiallyRecoverable += opp.AmountMinorUnits
		}
	}

	strategies := []EvaluationStrategy{
		NewFixedRetryStrategy(),
		NewStaticRulesStrategy(),
	}
	for _, p := range revGuardProfileKeys {
		strategies = append(strategies, NewRevGuardStrategyWithProfile(p.Key, p.Config))
	}

	strategyResults := make(map[string]StrategyMetrics, len(strategies))
	for _, strat := range strategies {
		decisions := make([]StrategyDecision, len(dataset.Opportunities))
		for i, opp := range dataset.Opportunities {
			decision, err := strat.Decide(opp)
			if err != nil {
				return nil, fmt.Errorf("service: strategy %q failed on opportunity %s: %w", strat.Name(), opp.ID, err)
			}
			decisions[i] = decision
		}
		strategyResults[strat.Name()] = aggregateStrategyMetrics(strat.Name(), dataset, decisions, revenueAtRisk)
	}

	comparisons := make(map[string]ComparisonResult, len(revGuardProfileKeys)*len(baselineStrategyNames))
	for _, p := range revGuardProfileKeys {
		profile := strategyResults[p.Key]
		for _, baselineName := range baselineStrategyNames {
			baseline := strategyResults[baselineName]
			comparisons[p.Key+"_vs_"+baselineName] = ComparisonResult{
				ProfileName:                           p.Key,
				BaselineName:                          baselineName,
				IncrementalRecoveredRevenueMinorUnits: profile.RevenueRecoveredMinorUnits - baseline.RevenueRecoveredMinorUnits,
				IncrementalNetValueMinorUnits:         profile.NetIncrementalValueMinorUnits - baseline.NetIncrementalValueMinorUnits,
				ActionReductionPercent:                actionReductionPercent(baseline.ActionsTaken, profile.ActionsTaken),
				IncrementalRecoveryRate:               profile.RecoveryRate - baseline.RecoveryRate,
			}
		}
	}

	return &EvaluationResult{
		Dataset: DatasetSummary{
			Seed:                                    seed,
			Opportunities:                           cases,
			Type:                                    "synthetic",
			RevenueAtRiskMinorUnits:                 revenueAtRisk,
			PotentiallyRecoverableRevenueMinorUnits: potentiallyRecoverable,
			Currency:                                "INR",
		},
		Strategies:  strategyResults,
		Comparisons: comparisons,
		Disclaimer:  evaluationDisclaimer,
	}, nil
}

func actionReductionPercent(baselineActionsTaken, profileActionsTaken int) float64 {
	if baselineActionsTaken == 0 {
		return 0
	}
	return float64(baselineActionsTaken-profileActionsTaken) / float64(baselineActionsTaken) * 100
}

// strategyDisplayOrder is the fixed row order for both human-readable
// renderers below, independent of Go's randomized map iteration order.
var strategyDisplayOrder = []string{"fixed_retry", "static_rules", "revguard_conservative", "revguard_balanced", "revguard_aggressive"}

// comparisonDisplayOrder is the fixed comparison-row order: every
// profile against every baseline, profile-major.
func comparisonDisplayOrder() []string {
	order := make([]string, 0, len(revGuardProfileKeys)*len(baselineStrategyNames))
	for _, p := range revGuardProfileKeys {
		for _, baselineName := range baselineStrategyNames {
			order = append(order, p.Key+"_vs_"+baselineName)
		}
	}
	return order
}

// FormatResultTable renders the human-readable CLI table: all five
// strategies (two baselines, three RevGuard profiles), then every
// profile-vs-baseline comparison.
func FormatResultTable(result *EvaluationResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "SYNTHETIC evaluation — seed=%d opportunities=%d (NOT live Razorpay data)\n\n", result.Dataset.Seed, result.Dataset.Opportunities)
	fmt.Fprintf(&b, "Revenue At Risk:               %12d %s\n", result.Dataset.RevenueAtRiskMinorUnits, result.Dataset.Currency)
	fmt.Fprintf(&b, "Potentially Recoverable:       %12d %s\n\n", result.Dataset.PotentiallyRecoverableRevenueMinorUnits, result.Dataset.Currency)

	fmt.Fprintf(&b, "%-22s %14s %8s %8s %10s %11s %10s %14s %10s %8s %8s\n",
		"Strategy", "Recovered", "Actions", "Blocked", "Escalated", "Unsupport.", "Ambiguous", "NetValue", "Unnecess.", "Rate%", "AvgAtt")
	fmt.Fprintln(&b, strings.Repeat("-", 148))

	for _, name := range strategyDisplayOrder {
		m, ok := result.Strategies[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%-22s %14d %8d %8d %10d %11d %10d %14d %10d %7.2f%% %8.2f\n",
			m.Name, m.RevenueRecoveredMinorUnits, m.ActionsTaken, m.ActionsBlocked, m.HumanEscalations,
			m.UnsupportedActions, m.AmbiguousOutcomes,
			m.NetIncrementalValueMinorUnits, m.UnnecessaryActions, m.RecoveryRate*100, m.AverageAttempts)
	}

	b.WriteString("\nComparison (each RevGuard profile vs. each baseline, same dataset):\n")
	for _, name := range comparisonDisplayOrder() {
		c, ok := result.Comparisons[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "  %-22s vs %-14s incremental_recovered_revenue=%d incremental_net_value=%d action_reduction=%.2f%% incremental_recovery_rate=%.4f\n",
			c.ProfileName, c.BaselineName, c.IncrementalRecoveredRevenueMinorUnits, c.IncrementalNetValueMinorUnits, c.ActionReductionPercent, c.IncrementalRecoveryRate)
	}

	b.WriteString("\n" + evaluationDisclaimer + "\n")

	return b.String()
}

// FormatMarkdownReport renders the human-readable Markdown evaluation
// report. generatedAt and commit are supplied by the caller (see
// cmd/evaluate/main.go) rather than embedded in EvaluationResult itself:
// a wall-clock timestamp or environment-derived commit hash would break
// EvaluationResult's determinism guarantee (RunEvaluation must be a pure
// function of (seed, cases) — see TestRunEvaluation_Reproducible). This
// function is purely presentational and has no bearing on the computed
// metrics.
func FormatMarkdownReport(result *EvaluationResult, generatedAt time.Time, commit string) string {
	if commit == "" {
		commit = "unknown"
	}

	var b strings.Builder

	b.WriteString("# RevGuard Evaluation Report\n\n")
	b.WriteString("**Synthetic evaluation — not production performance.**\n\n")
	b.WriteString(evaluationDisclaimer + "\n\n")

	b.WriteString("## Run metadata\n\n")
	fmt.Fprintf(&b, "- Generated at: %s\n", generatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Code version (git commit): `%s`\n", commit)
	fmt.Fprintf(&b, "- Seed: `%d`\n", result.Dataset.Seed)
	fmt.Fprintf(&b, "- Scenario count: `%d`\n", result.Dataset.Opportunities)
	fmt.Fprintf(&b, "- Dataset type: `%s`\n\n", result.Dataset.Type)

	b.WriteString("## Assumptions\n\n")
	b.WriteString("- All strategies below — both baselines and all three RevGuard policy profiles —\n" +
		"  are evaluated against the exact same synthetic dataset for this seed.\n")
	b.WriteString("- All monetary figures are integer minor units (paise), currency INR only.\n")
	b.WriteString("- The synthetic dataset's amount range, failure-category distribution, and every\n" +
		"  ground-truth coefficient are illustrative assumptions, not measured Razorpay data —\n" +
		"  see docs/architecture/evaluation-engine.md.\n")
	b.WriteString("- The ground-truth recoverability model is intentionally independent of RevGuard's\n" +
		"  own probability estimator (different base-rate table) so RevGuard cannot grade its own\n" +
		"  homework.\n")
	b.WriteString("- RevGuard's simulated execution stage respects ExecutionEngine's real, current\n" +
		"  limitation: only retry_payment and send_payment_link (Milestone 10) have execution\n" +
		"  implementations. A policy ALLOW for any other action is authorized but not executed\n" +
		"  (see \"unsupported_actions\").\n")
	b.WriteString("- A small, fixed, illustrative fraction of executed actions never produce a\n" +
		"  definitive financial-truth signal (mirrors Milestone 6/7's UNKNOWN outcome) and are\n" +
		"  reported separately as \"ambiguous_outcomes\", never guessed into success or failure.\n\n")

	b.WriteString("## Strategy definitions\n\n")
	b.WriteString("- **fixed_retry** — retries every opportunity via retry_payment up to a fixed\n" +
		"  attempt cap. No AI diagnosis, no economic optimization, no policy intelligence, no\n" +
		"  escalation concept.\n")
	b.WriteString("- **static_rules** — a fixed category -> action lookup table, gated by a fixed\n" +
		"  cooldown, amount ceiling, and attempt cap. Still no AI, no economic model, no escalation.\n")
	b.WriteString("- **revguard_conservative / revguard_balanced / revguard_aggressive** — the real,\n" +
		"  unmodified RevGuard pipeline (deterministic AI-diagnosis stand-in +\n" +
		"  HeuristicProbabilityEstimator + GetActionEconomics + the economic formulas, Milestone 4)\n" +
		"  feeding evaluatePolicyRules, evaluated under three different PolicyConfig profiles\n" +
		"  (Milestone 10) — identical rules, different thresholds. revguard_balanced uses the same\n" +
		"  numeric thresholds as the original Milestone 5 default policy. See\n" +
		"  docs/architecture/policy-engine.md for the exact per-profile values. Execution is\n" +
		"  further gated by ExecutionEngine's real retry_payment/send_payment_link-only coverage.\n\n")

	b.WriteString("## Results\n\n")
	b.WriteString(fmt.Sprintf("Revenue At Risk: %d %s\n\n", result.Dataset.RevenueAtRiskMinorUnits, result.Dataset.Currency))
	b.WriteString(fmt.Sprintf("Potentially Recoverable Revenue: %d %s\n\n", result.Dataset.PotentiallyRecoverableRevenueMinorUnits, result.Dataset.Currency))

	b.WriteString("| Strategy | Recovered | Recovery Rate | Actions Taken | Blocked | Escalated | Unsupported | Ambiguous | Unnecessary | Recovery Cost | Risk Cost | Expected Recovery Value | Net Value |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, name := range strategyDisplayOrder {
		m, ok := result.Strategies[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %.4f | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d |\n",
			m.Name, m.RevenueRecoveredMinorUnits, m.RecoveryRate, m.ActionsTaken, m.ActionsBlocked,
			m.HumanEscalations, m.UnsupportedActions, m.AmbiguousOutcomes, m.UnnecessaryActions,
			m.RecoveryCostMinorUnits, m.RiskCostMinorUnits, m.ExpectedRecoveryValueMinorUnits,
			m.NetIncrementalValueMinorUnits)
	}
	b.WriteString("\n")

	b.WriteString("## Comparison (each RevGuard profile vs. each baseline)\n\n")
	b.WriteString("| Profile | Baseline | Incremental Recovered Revenue | Incremental Net Value | Action Reduction % | Incremental Recovery Rate |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, name := range comparisonDisplayOrder() {
		c, ok := result.Comparisons[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %.2f%% | %.4f |\n",
			c.ProfileName, c.BaselineName, c.IncrementalRecoveredRevenueMinorUnits, c.IncrementalNetValueMinorUnits,
			c.ActionReductionPercent, c.IncrementalRecoveryRate)
	}
	b.WriteString("\n")

	b.WriteString("## Interpretation\n\n")
	b.WriteString("The comparison table above is computed directly from this run; it is not adjusted\n" +
		"or curated, and no policy profile's thresholds were chosen after seeing these results.\n" +
		"A positive Incremental Recovered Revenue means the named RevGuard profile recovered more\n" +
		"than the named baseline in this run; a negative value means it recovered less. Action\n" +
		"Reduction % reflects how many fewer (or more, if negative) actions that profile executed\n" +
		"relative to the baseline. Comparing the three profiles to each other (not just to the\n" +
		"baselines) shows the risk/recovery trade-off a merchant would face choosing between them.\n" +
		"See docs/architecture/evaluation-engine.md and CLAUDE.md's Milestone 10 section for the\n" +
		"specific numbers from the seed used above and an honest discussion of why they came out\n" +
		"the way they did.\n\n")

	b.WriteString("## Limitations\n\n")
	b.WriteString("- The AI diagnosis step is a deterministic rule-based stand-in, not a live LLM call.\n")
	b.WriteString("- The ground-truth model is an illustrative, uncalibrated assumption — RevGuard has\n" +
		"  no historical outcome data yet to calibrate against.\n")
	b.WriteString("- WebhookProcessor/ReconciliationEngine code paths are not invoked; the ground-truth\n" +
		"  and ambiguous-outcome models stand in for what those engines would determine.\n")
	b.WriteString("- ESCALATE never recovers revenue in this simulation (no human-approval workflow is\n" +
		"  modeled).\n")
	b.WriteString("- Currency is INR-only.\n\n")

	b.WriteString("**This report describes a synthetic evaluation. It is not, and must not be presented\n" +
		"as, a benchmark of real-world Razorpay production performance.**\n")

	return b.String()
}
