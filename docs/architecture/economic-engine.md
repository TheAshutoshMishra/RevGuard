# Economic Engine — Milestone 4

This document describes how RevGuard decides whether a diagnosed
recommendation has positive expected economic value. It covers only what
Milestone 4 implements: deterministic economic evaluation. Policy
decisions (whether to actually act on that evaluation) are Milestone 5
and are not described here beyond their integration point.

**The central rule: economic evaluation ≠ policy decision.** The Economic
Engine answers "does this recommendation look economically worthwhile?"
with a number. It never answers "should we do it?" — that requires
policy thresholds, risk appetite, and business rules that don't exist
yet. Evaluating a diagnosis never changes `RecoveryCase.Status`; a case
is `ANALYZED` before evaluation and `ANALYZED` after it.

## Problem being solved

By the end of Milestone 3, a `RecoveryCase` has an AI-generated
`RecoveryDiagnosis`: a recommended action (`retry_payment`,
`send_payment_link`, etc.) and a confidence score. That's a
recommendation, not an economic case for acting on it. Two recommendations
with identical confidence can have very different economics — a ₹50,000
payment and a ₹50 payment might both get `retry_payment` recommended at
0.8 confidence, but retrying the ₹50,000 payment is worth far more effort.
The Economic Engine translates a diagnosis into money: what's the revenue
at risk, how likely is it to actually come back, what would the action
cost, and is the expected outcome worth it.

## Why AI confidence is NOT recovery probability

This is the most important conceptual boundary in this milestone.

`RecoveryDiagnosis.Confidence` (Milestone 3) answers: **"how confident is
the AI in this recommendation?"** It is a property of the model's own
certainty — it says nothing about the customer, the payment method, or
historical outcomes for similar cases. A model can be very confident
about a bad recommendation, or unconfident about a good one.

`RecoveryEconomicEvaluation.RecoveryProbabilityBps` (this milestone)
answers a completely different question: **"if we take the recommended
action, what's the probability the revenue actually comes back?"** This
requires information about the failure category, the action, and the
case's history — not the AI's self-assessment.

The codebase enforces this separation structurally: `EconomicEngine`
never reads `diagnosis.Confidence`, and `RecoveryProbabilityEstimator`'s
interface doesn't expose it to implementations in a way that invites
using it as a shortcut (see `recovery_probability_estimator.go`'s
package-level doc comment). Conflating the two would let a model's
self-reported certainty silently substitute for an actual probability
estimate — precisely the failure mode this design prevents.

## Economic model

All monetary values are `int64` minor units with an explicit currency,
via `domain.Money` — never float/double, for the same reason as
everywhere else in this codebase (Milestone 1's `domain.Money` doc
comment). Probabilities and cost rates are integer basis points
(`domain.ProbabilityBasisPoints`: 0 = 0%, 10000 = 100%), never float,
for the same determinism guarantee.

### Formulas

```
expected_gross_recovery = revenue_at_risk * recovery_probability_bps / 10000
risk_cost                = revenue_at_risk * risk_cost_bps / 10000
expected_incremental_value = expected_gross_recovery - action_cost - risk_cost
```

Implemented in `backend/internal/service/economic_calculations.go` as
pure functions with no I/O, unit-tested directly (`economic_calculations_test.go`,
an internal `package service` test file so it can call the unexported
`calculate*` functions).

**Rounding**: all division is standard Go integer division on
non-negative operands, which truncates toward zero — equivalent to floor
division. This is the only rounding rule anywhere in the engine; no
value is ever adjusted after the fact, and no floating-point arithmetic
is introduced at any point.

**Signedness**: `revenue_at_risk`, `expected_gross_recovery`,
`action_cost`, and `risk_cost` are all non-negative by construction and
modeled as `domain.Money` (which rejects negative amounts).
`expected_incremental_value` is deliberately **not** `domain.Money` —
it's a plain signed `int64` — because a negative result (the action costs
more than it's expected to recover) is a valid, important outcome that
the engine must be able to represent and persist.

**These formulas and the default coefficients below are RevGuard's own
demonstrable economic model. They are not derived from Razorpay's actual
economics, are not measured production benchmarks, and are not the
product of any historical data analysis.**

## Recovery probability estimator

`RecoveryProbabilityEstimator` (`recovery_probability_estimator.go`) is
an interface:

```go
type RecoveryProbabilityEstimator interface {
    Estimate(ctx, recoveryCase, diagnosis, paymentAttempts, priorRecoveryActions) (ProbabilityEstimate, error)
}
```

Milestone 4 ships one implementation, `HeuristicProbabilityEstimator`
(`estimator_name = "heuristic"`, `estimator_version = "heuristic-v1"`).
**It is explicitly not machine learning** and makes no external calls
(not to the AI service, not anywhere) — it's a transparent, documented,
deterministic rule:

```
base_bps       = heuristicBaseRateBps[failure_category]      // illustrative per-category prior
multiplier     = heuristicActionMultiplierPercent[action]    // illustrative per-action modifier
adjusted       = base_bps * multiplier / 100

attempt_penalty       = max(0, len(payment_attempts) - 1) * 500   // bps, diminishing returns
prior_action_penalty  = len(prior_recovery_actions) * 800         // bps, repeated attempts imply harder case

result = clamp(adjusted - attempt_penalty - prior_action_penalty, 0, 10000)
```

Every constant (`heuristicBaseRateBps`, `heuristicActionMultiplierPercent`,
the two penalty constants) is a documented, illustrative **assumption**,
not a measured benchmark — see the comments directly above each map in
`recovery_probability_estimator.go`. `stop_recovery`'s multiplier is 0%:
recommending to stop implies no further recovery is expected, so the
probability is always 0 regardless of failure category.

The estimator is a pure function: the same
case/diagnosis/attempts/actions always produce the same
`ProbabilityEstimate` (`TestHeuristicEstimator_Deterministic` asserts
this directly). `EstimatorName`/`EstimatorVersion` are recorded on every
persisted evaluation for reproducibility — see "Versioning" below.

**Future work**: this heuristic is a placeholder for a probability model
calibrated against real historical recovery outcomes once RevGuard has
enough of them. Replacing it means implementing a new
`RecoveryProbabilityEstimator` and swapping it in `main.go` — the
`EconomicEngine`'s interface and the persisted schema do not need to
change.

## Action economics

`ActionEconomics` (`action_economics.go`) is a small, explicit,
versioned table — one entry per `domain.RecommendedAction` — giving:

- `ActionCostMinorUnits`: a fixed assumed operational cost of attempting
  the action (gateway fee, messaging cost, agent time).
- `RiskCostBps`: the action's assumed downside risk, as a proportion of
  revenue at risk (same basis-points-of-revenue convention as recovery
  probability, for consistent scaling).

```
retry_payment:                  cost=500,  risk=50 bps
send_payment_link:               cost=200,  risk=30 bps
request_payment_method_change:   cost=300,  risk=40 bps
send_reminder:                   cost=50,   risk=10 bps
escalate_to_human:                cost=5000, risk=20 bps
stop_recovery:                   cost=0,    risk=0 bps
```

Every value is documented in-line as an illustrative RevGuard-v1
assumption, not a real Razorpay cost. `GetActionEconomics` rejects any
action not in the table (`ErrUnknownRecommendedAction`) rather than
silently defaulting — this can only happen for a value outside
`domain.RecommendedAction`'s six-value vocabulary, which the database
`CHECK` constraint on `recovery_diagnoses.recommended_action` already
prevents from existing in a stored diagnosis in the first place.

`EconomicModelVersion = "economic-model-v1"` identifies this table
together with the formulas above as one versioned unit; bump it whenever
either changes in a way that would make past evaluations
non-reproducible under the new logic.

**Known limitation**: `ActionCostMinorUnits` is applied as a flat minor-
units value regardless of the evaluation's actual currency (RevGuard is
INR-only in practice via Milestone 1's `Payment` model, so these are
effectively paise). A future milestone could make action costs
currency-aware; this milestone does not attempt it, since introducing
that now — with only one currency ever exercised — would be unverifiable
speculation.

## Persistence: `domain.RecoveryEconomicEvaluation`

Migration `000013` adds `recovery_economic_evaluations` — a new table,
not a modification of any Milestone 1–3 table. Deliberately separate from
`recovery_diagnoses` (a recommendation) and `recovery_actions` (an
authorized/executed action, still unused as of this milestone): an
economic evaluation is neither.

Every row records `provider`-equivalent versioning metadata —
`estimator_name`, `estimator_version`, `economic_model_version` — plus
`created_at`, so a stored evaluation is reproducible and attributable,
matching the pattern `RecoveryDiagnosis` established in Milestone 3 for
`provider`/`model`/`prompt_version`. No raw AI provider response is
stored (there wouldn't be one to store — the engine calls no external
service).

## Idempotency

`UNIQUE(recovery_diagnosis_id)` (migration `000013`) is the durable
authority: at most one evaluation exists per diagnosis, enforced by
PostgreSQL, not Redis. `RecoveryEconomicEvaluationRepository.TryCreate`
uses `INSERT ... ON CONFLICT (recovery_diagnosis_id) DO NOTHING` — the
same pattern `recovery_events.event_id` idempotency used in Milestone 2 —
which never raises an error and therefore never poisons the enclosing
transaction, unlike a plain `INSERT` hitting a real unique-violation
error. `EconomicEngine.Evaluate` checks for an existing evaluation before
computing anything (avoiding redundant work on the common path), and
falls back to re-reading after a lost race on `TryCreate` (the same
transaction stays usable either way, so no `SAVEPOINT` is needed here —
contrast with `RecoveryOrchestrator`'s case-creation race in Milestone 2,
which does use one because that path uses a plain `INSERT` that can
error).

A new diagnosis (e.g. from re-analysis, once some future milestone
reopens a case to `ANALYZING`) gets its own, independent evaluation row —
evaluations are never overwritten.

## Audit trail

Every newly created evaluation writes one `AuditEvent`
(`event_type = "recovery_economics.evaluated"`, `actor_type = SYSTEM`)
with metadata covering the diagnosis ID, recommended action, probability,
all four monetary figures, and the three version identifiers. An
idempotent no-op (evaluation already existed) does not write a second
audit row, matching how Milestone 2 doesn't re-audit duplicate event
processing.

## Lifecycle integration

```
DETECTED -> ANALYZING -> ANALYZED (Milestones 2-3)
                             |
                             v
                    EconomicEngine.Evaluate
                             |
                             v
                    remain ANALYZED
```

`EventProcessor.Process` calls `EconomicEvaluator.Evaluate` (an interface
satisfied by `*EconomicEngine`) immediately after a successful AI
analysis (`result.Analyzed && result.Diagnosis != nil`) — see
`event_processor.go`. This is a distinct step from `AnalysisOrchestrator`,
not a change to it: `AnalyzeCase`'s two-phase structure exists because it
makes a slow external call (the AI service); `EconomicEngine.Evaluate`
makes no external call at all, so it does all of its work — reads and
writes — inside one short transaction (see `economic_engine.go`'s
package-level doc comment for the contrast).

Economic evaluation failure does not fail the `POST /events` request,
following the same pattern established for AI analysis failure in
Milestone 3: the event was durably ingested, the case was durably
created and analyzed regardless. The response carries
`EconomicEvaluation`/`EconomicEvaluationError` on `ProcessResult`
separately from the event-processing outcome.

**Milestone 4 stops here.** Nothing in this codebase currently reads
`RecoveryEconomicEvaluation` to make a decision. `POLICY_CHECK` and
`ALLOW`/`BLOCK`/`ESCALATE` remain state-machine vocabulary only (declared
since Milestone 2, never reached). Milestone 5 will read the evaluation
this milestone produces and decide.

## Why the engine does not make policy decisions

`EconomicEngine` computes a number (`expected_incremental_value`) and
persists it. It does not compare that number to any threshold, does not
call `ValidateTransition` toward `POLICY_CHECK`/`ALLOW`/`BLOCK`/`ESCALATE`,
and has no code path that could execute a payment action, call Razorpay,
or call a webhook. A negative `expected_incremental_value` is recorded
exactly the same way a positive one is — the engine has no opinion about
what should happen next. Deciding what to do with a negative (or
positive) value — including what thresholds matter, whether confidence
or risk flags should factor in, and whether human escalation should
override the number — is Milestone 5's policy engine, deliberately kept
out of this milestone's scope.

## Known assumptions

- Failure-category base rates, action multipliers, and penalty constants
  in `HeuristicProbabilityEstimator` are illustrative, not measured.
- `ActionEconomics` cost/risk values are illustrative RevGuard-v1
  demonstration defaults, not real Razorpay costs.
- Action costs are applied without currency conversion (see "Known
  limitation" under Action economics above).
- The heuristic estimator uses only case-local signals (failure category,
  recommended action, attempt/action counts) — it has no access to
  cross-case historical outcomes, because RevGuard does not yet durably
  record recovery outcomes at the volume needed to calibrate one.

## Known limitations

- No real historical probability calibration exists yet (see "Future
  work" above).
- Action economics are a fixed Go table, not a database-backed or
  merchant-configurable model (explicitly out of scope per this
  milestone's technology-discipline constraints — no new configuration
  system was introduced).
- The GET read endpoint (`GET /v1/recovery-cases/{id}/economic-evaluation`)
  returns only the latest evaluation for a case; retrieving full
  evaluation history for a case (e.g. across multiple diagnoses) requires
  a direct query for now.
