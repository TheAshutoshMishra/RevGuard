# Evaluation & Revenue Recovery Proof — Milestones 8–9

This document describes RevGuard's offline, deterministic evaluation
harness: the system that answers, with a computed number rather than a
claim, **"does RevGuard recover more incremental revenue than a simpler
recovery strategy, while taking fewer unnecessary or risky actions?"**

**Everything in this document and every number this system produces is
SYNTHETIC.** No real Razorpay, merchant, or customer data is used
anywhere in this evaluation, and no result computed here has been validated
against live production data. See "Why this is not production benchmark
data" at the end of this document.

## Purpose

Milestones 2–7 built the actual RevGuard decision pipeline: an event
triggers AI diagnosis, an economic evaluation, a policy decision, bounded
execution, and — via webhooks/reconciliation — a trusted financial
outcome. That pipeline makes real decisions, but nothing in the codebase
before Milestone 8 could answer whether those decisions are *better* than
a naive alternative, in dollar (or paisa) terms.

Milestone 8 does not add product features. It adds a proof layer: a
simulation that runs the same synthetic recovery opportunities through
RevGuard's real decision logic and through two independently-implemented
baseline strategies, applies one shared, strategy-blind ground-truth
outcome model, and reports the difference.

## Core thesis

> Optimize incremental recovered revenue, not number of retries/actions.

    Incremental Recovered Revenue = RevGuard recovered revenue
                                     - baseline recovered revenue

A strategy that recovers slightly less money but takes far fewer
actions may still be economically preferable once cost and risk are
counted — that tradeoff is exactly what this evaluation is built to
surface, not to hide.

## Milestone 9 refinements

Milestone 9 kept Milestone 8's architecture, dataset generator, and
baselines unchanged and closed two real production-fidelity gaps in how
RevGuard's simulated strategy translated a policy decision into a
financial outcome:

1. **Execution-capability gating.** Milestone 6's `ExecutionEngine` only
   ever implemented `retry_payment` — every other authorized action is
   rejected with `ErrActionNotExecutable` before any side effect (see
   `docs/architecture/execution-engine.md`). Milestone 8's simulation did
   not model this: any policy `ALLOW` was treated as if it executed.
   Milestone 9 added `isRevGuardActionExecutable` (mirroring
   `ExecutionEngine.phase1`'s exact check) so that an `ALLOW` for, say,
   `send_payment_link` is recorded as genuinely authorized
   (`StrategyDecision.Outcome == ALLOW`) but not executed
   (`StrategyDecision.Executed == false`, zero cost, zero possible
   recovery) — tallied under the new `UnsupportedActions` metric. This
   applies only to `RevGuardStrategy`; the baselines don't route through
   RevGuard's `ExecutionEngine` and are not bound by its current
   implementation coverage (see `StrategyDecision.Executed`'s doc
   comment in `evaluation_strategies.go`).
2. **Ambiguous/UNKNOWN financial outcomes.** Milestone 8's ground truth
   was strictly binary (recoverable or not). Real Milestone 6/7 behavior
   includes a third, genuinely different outcome: an executed action
   whose financial truth never resolves (a provider timeout at execution
   time, or an unresolved reconciliation lookup) — recorded as `UNKNOWN`
   and never guessed into `SUCCESS`/`FAILED`. Milestone 9 added
   `groundTruthResult.ObservationAmbiguous` (an independent, deterministic
   per-opportunity draw, illustrative 4% rate) and
   `resolveFinancialOutcome`, which reuses `domain.RecoveryOutcomeStatus`'s
   exact `SUCCESS`/`FAILED`/`UNKNOWN` vocabulary (Milestone 1/7) rather
   than inventing a parallel one. An `UNKNOWN` outcome is tallied under
   the new `AmbiguousOutcomes` metric — never counted as recovered, and
   never counted as `UnnecessaryActions` (unresolved is not the same as
   definitively wasted).

Two new metrics were added as a direct consequence:
`ExpectedRecoveryValueMinorUnits` (RevGuard's Economic Engine's ex-ante
prediction, recorded whenever policy `ALLOW`s — even an unsupported
action — so it can be compared against what was actually, definitively
recovered) and `IncrementalRecoveryRate` (`revguard.RecoveryRate -
baseline.RecoveryRate`, a ratio-of-ratios distinct from the absolute
`IncrementalRecoveredRevenueMinorUnits`). See "Metrics and exact
formulas" below for the full, current list.

Because both changes make RevGuard's simulated recovery *more*
conservative (a real action can now correctly recover nothing when it
isn't actually executable or when its outcome never resolves), Milestone
9's numbers for RevGuard are lower than Milestone 8's for the same seed —
this is the direct, intended effect of aligning the simulation with
production reality more closely, not a regression. See CLAUDE.md's
Milestone 9 section for the exact before/after figures.

## Architecture

```
seed, cases
  |
  v
GenerateSyntheticDataset(seed, cases)          [evaluation_dataset.go]
  |         \
  |          -> computeGroundTruth(opp, rng)   [evaluation_ground_truth.go]
  |               (independent per-opportunity RNG stream; NEVER
  |                exposed to any strategy)
  v
[]SyntheticOpportunity  (input features only)
  |
  |-----------------+-----------------+
  v                 v                 v
FixedRetryStrategy  StaticRulesStrategy  RevGuardStrategy
(baseline 1)        (baseline 2)         (reuses the real
                                          pipeline components)
  |                 |                 |
  v                 v                 v
StrategyDecision{Outcome: ALLOW|BLOCK|ESCALATE, Executed, Action, costs}
  |                 |                 |
  +--------+--------+--------+--------+
           v
  apply the SAME groundTruthResult (computed before any strategy ran)
  -> resolveFinancialOutcome(truth) for every Executed decision
     (SUCCESS | FAILED | UNKNOWN)
           v
  aggregateStrategyMetrics(...)   [evaluation_metrics.go]
           v
  RunEvaluation(...) -> EvaluationResult   [evaluation_engine.go]
           v
  JSON + FormatResultTable + FormatMarkdownReport (human-readable)
```

Every box above lives in `backend/internal/service/evaluation_*.go`.
`backend/cmd/evaluate/main.go` is a thin CLI wrapper — it does no
computation of its own.

**No I/O.** The entire pipeline in this milestone opens no database
connection, makes no HTTP call, and needs neither PostgreSQL nor the AI
service running. This is a deliberate scope boundary: M8 is a pure,
in-process simulation over the deterministic parts of the real
architecture, not an end-to-end system test (M0–M7's existing manual
smoke tests already cover that).

## Synthetic dataset

`GenerateSyntheticDataset(seed, count)` produces `count`
`SyntheticOpportunity` values. Each one models a single failed-payment
recovery opportunity with:

- `AmountMinorUnits` / `Currency` — INR 50.00 to INR 5,000.00, integer
  minor units, uniformly distributed.
- `FailureCategory` — one of the 7 values in
  `domain.ValidFailureCategories`, uniformly distributed.
- `PaymentMethod` — one of `card`, `upi`, `netbanking`, `wallet`, `emi`
  (an illustrative set, not a real Razorpay method vocabulary).
- `CustomerPriorSuccessfulPayments` / `CustomerPriorFailedPayments`.
- `PreviousAttempts` (1–4) and `PreviousRecoveryActions` (0–3) — mirror
  `PolicyRuleInput.PaymentAttemptCount` / `.PriorRecoveryActionCount`.
- `HoursSinceFailure` (0–168).

**Determinism.** Every random draw is made by a `*rand.Rand` derived
from `(seed, opportunity index, purpose salt)` via `deriveRand` — never
from a single shared, sequential generator. This means:

- The same `(seed, count)` always produces byte-identical opportunities
  and ground truths (`TestGenerateSyntheticDataset_SameSeedIsIdentical`).
- Generating an opportunity's features and computing its ground truth
  draw from two independent streams (`saltOpportunity` vs.
  `saltGroundTruth`), so neither can accidentally leak into the other.

The dataset is labeled `Type: "synthetic"` on both `SyntheticDataset` and
the top-level `EvaluationResult.Dataset.Type` — there is no code path
that produces any other value.

## Ground-truth model

`computeGroundTruth(opp, rng)` (`evaluation_ground_truth.go`) decides,
independently of any strategy, whether a given opportunity was actually
recoverable. It is computed **once per opportunity, before any strategy
runs**, and stored on an unexported field of `SyntheticDataset`
(`groundTruths`) that no `EvaluationStrategy.Decide(opportunity)`
implementation can reach — the interface signature only ever receives
the opportunity's input features.

Formula (illustrative, not measured Razorpay data):

```
score = groundTruthBaseRateBps[category]
      + groundTruthPaymentMethodModifierBps[method]
      + min(prior_successful_payments * 20, 1000)
      - prior_failed_payments * 50
      - max(previous_attempts - 1, 0) * 400
      - (hours_since_failure / 24) * 100
score = clamp(score, 0, 10000)
recoverable = (random_draw_in[0,10000) < score)
```

**Deliberately independent from RevGuard's own estimator.**
`groundTruthBaseRateBps` uses different numbers from
`heuristicBaseRateBps` (`recovery_probability_estimator.go`, Milestone
4). If the ground truth used RevGuard's own assumption table, RevGuard's
internal probability estimate would always exactly match reality by
construction — which would artificially favor RevGuard in the
comparison. Using a separately-tuned table means RevGuard's estimator is
imperfect against ground truth, the same way it is imperfect against
real-world outcomes it can never observe in advance.

## Baseline strategies

Both baselines are implemented independently of `RevGuardStrategy` and
of each other — they share only `GetActionEconomics` (the real,
Milestone-4 cost/risk table), because the cost of actually performing a
given action is a real fact about the action, not a strategic choice.

**Baseline 1 — Fixed Retry** (`FixedRetryStrategy`,
`evaluation_strategies.go`): retries every opportunity via
`retry_payment` unless `PreviousAttempts >= 3`. No AI diagnosis, no
economic optimization, no policy intelligence, and no escalation concept
at all (`Outcome` is only ever `ALLOW` or `BLOCK`).

**Baseline 2 — Static Rules** (`StaticRulesStrategy`): a fixed lookup
table (`transient_failure -> retry_payment`,
`insufficient_funds -> send_payment_link`) gated by a fixed 2-hour
cooldown, a fixed INR 2,000.00 amount ceiling, and a fixed 2-attempt
maximum. Still no AI, no economic model, no escalation.

## RevGuard strategy

`RevGuardStrategy` (`evaluation_strategies.go`) is **not** a second,
parallel implementation of RevGuard's logic. It calls the exact same
unmodified functions the real HTTP pipeline calls:

1. `deterministicDiagnosis(opp)` (`evaluation_diagnosis.go`) — stands in
   for the AI service HTTP call. It deliberately mirrors
   `ai-service/app/providers/mock_provider.py`'s rule priority and
   confidence values (the project's own existing, deterministic,
   explicitly-not-real-AI provider), re-expressed against
   `SyntheticOpportunity` fields. This is what "AI recommends" means for
   an evaluation that must be reproducible with no network access — see
   "Known limitations" below for exactly what this does and doesn't
   prove.
2. `HeuristicProbabilityEstimator.Estimate` — **Milestone 4, untouched.**
3. `GetActionEconomics` + `calculateExpectedGrossRecovery` /
   `calculateRiskCost` / `calculateExpectedIncrementalValue` —
   **Milestone 4, untouched.**
4. `evaluatePolicyRules` + `DefaultPolicyConfig` — **Milestone 5,
   untouched.**

The result is translated into a `StrategyDecision` using
`domain.PolicyDecisionOutcome`'s exact `ALLOW`/`BLOCK`/`ESCALATE`
vocabulary (Milestone 5) — the same three outcomes the real
`PolicyEngine` produces. RevGuard never gets to alter the ground truth,
never sees the baselines' decisions, and never sees its own outcome
before the ground truth is applied — the exact same
`resolveFinancialOutcome(truth)` function (`evaluation_metrics.go`) is
used for every strategy's executed decisions.

5. `isRevGuardActionExecutable` — **Milestone 9, new.** Mirrors
   `ExecutionEngine.phase1`'s exact check
   (`decision.AuthorizedAction != domain.RecommendedActionRetryPayment`
   -> `ErrActionNotExecutable`). An `ALLOW` for `retry_payment` sets
   `StrategyDecision.Executed = true` with real cost/risk; an `ALLOW`
   for anything else stays authorized (`Outcome == ALLOW`, `Action` set)
   but `Executed = false`, zero cost, zero possible recovery — see
   "Milestone 9 refinements" above.

**Why not the real `AnalysisOrchestrator`/`EconomicEngine`/
`PolicyEngine`/`ExecutionEngine`?** Those engines are correct and
untouched, but they are wired to PostgreSQL (`repository.DBTX`) and, for
`AnalysisOrchestrator`, to a live AI-service HTTP call — necessary for
the real system's auditability and idempotency guarantees, but
incompatible with "deterministic and reproducible from a fixed seed,
no unnecessary dependencies" for 500–1,000 opportunities run
repeatedly. This evaluation reuses every *deterministic, pure*
computation those engines are built from, and does not reimplement or
approximate any of their formulas.

## Simulation

For every opportunity, `RunEvaluation` (`evaluation_engine.go`):

1. Calls `Decide(opportunity)` on all three strategies — the same
   opportunity value, unchanged.
2. For every `ALLOW` decision, resolves the executed/unsupported split
   (`decision.Executed`) and, for executed decisions, applies the same
   precomputed `groundTruthResult` via `resolveFinancialOutcome`
   (SUCCESS / FAILED / UNKNOWN).
3. Aggregates: recovered revenue, cost, risk cost, expected recovery
   value, actions taken/blocked/escalated/unsupported, ambiguous
   outcomes, unnecessary actions, average attempts
   (`aggregateStrategyMetrics`).
4. Compares RevGuard against each baseline.

## Metrics and exact formulas

All monetary figures are `int64` minor units. `RecoveryRate`,
`AverageAttempts`, `ActionReductionPercent`, and `IncrementalRecoveryRate`
are display ratios, not money, and are the only `float64` fields — the
same convention `domain.RecoveryDiagnosis.Confidence` already
established.

| Metric | Formula |
|---|---|
| Revenue At Risk | `sum(opportunity.AmountMinorUnits)` over the whole dataset |
| Potentially Recoverable Revenue | `sum(opportunity.AmountMinorUnits)` where `groundTruth.Recoverable == true` |
| Revenue Recovered | `sum(opportunity.AmountMinorUnits)` where `decision.Executed == true AND resolveFinancialOutcome(truth) == SUCCESS` |
| Recovery Rate | `RevenueRecovered / RevenueAtRisk` (0 if RevenueAtRisk is 0) |
| Incremental Recovered Revenue | `revguard.RevenueRecovered - baseline.RevenueRecovered` |
| Recovery Cost | `sum(decision.ActionCostMinorUnits)` (0 unless Executed) |
| Risk Cost | `sum(decision.RiskCostMinorUnits)` (0 unless Executed) |
| Expected Recovery Value | `sum(decision.ExpectedGrossRecoveryMinorUnits)` over every `Outcome == ALLOW` (executed or not); always 0 for the baselines, which have no economic model |
| Net Incremental Value | `RevenueRecovered - RecoveryCost - RiskCost` |
| Incremental Net Value | `revguard.NetIncrementalValue - baseline.NetIncrementalValue` |
| Actions Taken | count of `Executed == true` |
| Actions Blocked | count of `Outcome == BLOCK` |
| Human Escalations | count of `Outcome == ESCALATE` |
| Unsupported Actions | count of `Outcome == ALLOW AND Executed == false` (Milestone 6's real retry_payment-only execution coverage) |
| Ambiguous Outcomes | count of `Executed == true AND resolveFinancialOutcome(truth) == UNKNOWN` |
| Unnecessary Actions | count of `Executed == true AND resolveFinancialOutcome(truth) == FAILED` |
| Average Attempts | mean of `opportunity.PreviousAttempts` over opportunities where `Executed == true` (0 if none) |
| Action Reduction % | `(baseline.ActionsTaken - revguard.ActionsTaken) / baseline.ActionsTaken * 100` (0 if baseline took no actions) |
| Incremental Recovery Rate | `revguard.RecoveryRate - baseline.RecoveryRate` |

Every opportunity falls into exactly one of Actions Blocked / Human
Escalations / Unsupported Actions / (executed: SUCCESS, contributing to
Revenue Recovered) / Unnecessary Actions (FAILED) / Ambiguous Outcomes
(UNKNOWN) — never more than one, so nothing is double-counted (see
`TestAggregateStrategyMetrics_NoDoubleCounting`).

These formulas are not redefined anywhere else in the code; the table
above is the single source of truth for them.

**A note on ESCALATE.** No human-approval workflow exists yet (that's
explicitly out of scope — see "Strictly out of scope" in the milestone
brief and Milestone 5/6's own documented scope boundaries). An
`ESCALATE` decision in this simulation never recovers money and never
incurs a cost: it is counted only in `HumanEscalations`. This
understates what a real human-in-the-loop process might recover: it
does not (and should not) simulate one.

## Reproducibility

```
go run ./cmd/evaluate --seed 12345 --cases 1000
go run ./cmd/evaluate --seed 12345 --cases 1000 --output evaluation.json
go run ./cmd/evaluate --seed 12345 --cases 1000 \
  --output evaluation.json --markdown-output evaluation.md \
  --commit $(git rev-parse --short HEAD)
```

Running the same command twice with the same `--seed`/`--cases`
produces byte-identical JSON — verified both by
`TestRunEvaluation_Reproducible` (which additionally does a `reflect.DeepEqual`
of the full result, not just the JSON) and by a manual run of the CLI
twice with a file diff (see the CLAUDE.md Milestone 9 verification
section). No wall-clock timestamp, random UUID, or other non-deterministic
value appears anywhere in `EvaluationResult`. `--commit` and the Markdown
report's "Generated at" timestamp are supplied by the caller and rendered
only by `FormatMarkdownReport` — a presentational function that reads
`EvaluationResult` but is not part of it, so the JSON stays reproducible
even though the Markdown report's metadata line legitimately differs run
to run (verified by diffing the JSON, not the Markdown, across two runs
with different `--commit` values).

## Fairness / anti-bias guarantees

- **All strategies see the same dataset.** `RunEvaluation` ranges over
  one `dataset.Opportunities` slice and calls every strategy's `Decide`
  with the same value; no strategy receives a copy, subset, or
  strategy-specific view (`TestFairness_AllStrategiesSeeSameDataset`).
- **Ground truth is strategy-independent.** Computed once, before any
  strategy runs, from an RNG stream no strategy can reach
  (`TestGroundTruth_IndependentOfStrategy`).
- **RevGuard cannot alter ground truth.** `groundTruthResult` is never
  passed into any `Decide` call and `SyntheticDataset.groundTruths` is
  unexported; `RevGuardStrategy.Decide`'s signature makes this
  structurally impossible, not just a convention.
- **Baselines cannot see RevGuard's decisions.** Every strategy's
  `Decide` is a pure function of the opportunity alone — no shared
  mutable state, no strategy holds a reference to another
  (`TestFairness_BaselinesCannotSeeRevGuardDecisions`,
  `TestFairness_StrategyDecisionOrderIndependent`).
- **Synthetic data is clearly labeled** at every level:
  `SyntheticDataset.Type`, `EvaluationResult.Dataset.Type`, and
  `EvaluationResult.Disclaimer` all say so; the CLI table's first line
  and last line both restate it.
- **No production Razorpay claims are generated.** Nothing in this
  package's output ever asserts live-production validation — see the
  `Disclaimer` constant in `evaluation_engine.go` and
  `TestFairness_NoProductionRazorpayClaims`.

## Known limitations

- **The AI diagnosis step is a deterministic stand-in, not a live AI
  call.** `deterministicDiagnosis` mirrors `ai-service`'s own
  `MockProvider` rules (already the project's standard "not real AI"
  fixture) rather than invoking a real LLM. A real AI service call would
  break both reproducibility (non-deterministic model output) and the
  "no unnecessary dependencies / stdlib-only" requirement for a
  1,000-opportunity evaluation run offline. This means the evaluation
  measures RevGuard's *economic and policy* logic faithfully, but does
  **not** measure real AI diagnosis quality.
- **The ground-truth model is an illustrative assumption, not measured
  recovery data.** RevGuard has not yet executed any real actions at
  scale (Milestone 6/7 are proven correct, not yet run at volume), so
  there is no historical outcome data to calibrate against — the same
  documented limitation as the Economic Engine's own probability
  estimator (see `docs/decisions/0001-economic-engine-probability-vs-confidence.md`).
- **`ExecutionEngine`/`WebhookProcessor`/`ReconciliationEngine` are not
  invoked.** The simulation models "was money actually recovered" via
  the ground-truth model plus `resolveFinancialOutcome` and
  `isRevGuardActionExecutable`, standing in for what M6 execution + M7
  financial truth would determine for a real action — as of Milestone 9
  this stand-in faithfully respects M6's real retry_payment-only
  execution coverage and models M7's UNKNOWN outcome as a distinct,
  never-guessed case, but it still does not exercise the real
  execution/webhook/reconciliation *code paths themselves*, which are
  already covered by their own Milestone 6/7 test suites. This remains a
  deliberate simplification for a fast, dependency-free, in-process
  evaluation, not a claim that the real engines were run.
- **The ambiguous-outcome rate (4%) is illustrative, not measured.**
  Milestone 7 has no automatic background reconciliation, so a nonzero
  rate of permanently-unresolved outcomes is a real property of the
  current system, but the specific 4% figure is an assumption, not a
  measured Razorpay reliability statistic.
- **ESCALATE never recovers revenue in this simulation** (see above) —
  a conservative simplification, not a claim that human escalation is
  worthless.
- **Currency is INR-only**, matching every prior milestone's stated
  limitation.
- **The illustrative amount range (INR 50–5,000) sits mostly above
  `DefaultPolicyConfig.MaxAutoAmountMinorUnits` (INR 1,000)**, which is
  itself an illustrative Milestone 5 default, not a tuned production
  threshold. In a default-configuration run, this means RevGuard
  escalates a large share of higher-value opportunities to (unmodeled)
  human review rather than acting automatically — a real, intentional
  safety property of the actual policy engine, not an evaluation
  artifact. It also means RevGuard's raw *automated* recovered revenue
  can come out lower than a reckless fixed-retry baseline's in this
  specific configuration. That is a genuine, non-rigged result of the
  actual policy thresholds against this particular dataset shape — see
  the exact figures recorded in CLAUDE.md's Milestone 8 section, and
  compare `ActionsBlocked`/`HumanEscalations`/`UnnecessaryActions`
  across strategies before drawing a conclusion from `RevenueRecovered`
  alone.

## Why this is not production benchmark data

Every number produced by `go run ./cmd/evaluate` comes from a
synthetically generated dataset with illustrative distributions, run
through a deterministic stand-in for AI diagnosis and an independently
assumed (not measured) ground-truth model. It is a **methodology and
correctness proof** — that the harness is reproducible, unbiased between
strategies, and that RevGuard's real economic/policy pipeline is wired
in without alteration — not a benchmark of real-world recovery
performance. No result from this milestone may be presented as
validated against live Razorpay production data, and none was.
