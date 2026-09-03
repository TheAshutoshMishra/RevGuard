# ADR 0001: Separate recovery probability from AI confidence, and evaluate economics before policy

- Status: Accepted
- Date: 2026-09-03
- Milestone: 4 — Economic Engine

## Context

Milestone 3 gave every `RecoveryCase` a `RecoveryDiagnosis` with a
`Confidence` field (0.0–1.0): the AI model's self-reported confidence in
its own recommendation. Milestone 4 needed to decide whether a
recommended action is economically worthwhile — which requires an
estimate of how likely the revenue actually is to be recovered.

The obvious shortcut was to reuse `RecoveryDiagnosis.Confidence` directly
as the recovery probability in the economic formulas. It was already
present, already validated, already in `[0, 1]`, and reusing it would
have meant zero new abstractions.

We rejected that shortcut.

## Decision

1. **Recovery probability is a distinct concept from AI confidence, with
   its own type, its own abstraction, and its own persisted field.**
   `RecoveryProbabilityEstimator` is a new interface
   (`backend/internal/service/recovery_probability_estimator.go`), never
   backed by `RecoveryDiagnosis.Confidence`. Its default implementation,
   `HeuristicProbabilityEstimator`, is a documented, versioned,
   deterministic rule-based estimator (`heuristic-v1`) — explicitly not
   machine learning, and making no calls to the AI service or anywhere
   else.

2. **The Economic Engine evaluates; it does not decide.**
   `EconomicEngine.Evaluate` computes and persists
   `RecoveryEconomicEvaluation` (revenue at risk, recovery probability,
   expected gross recovery, action cost, risk cost, expected incremental
   value) and stops. It never transitions `RecoveryCase.Status`, never
   compares the result to a threshold, and has no code path toward
   `POLICY_CHECK`/`ALLOW`/`BLOCK`/`ESCALATE` or any action execution.

## Rationale

**Why separate probability from confidence:** these answer different
questions. "How confident is the model in this recommendation?" and "how
likely is this money to actually come back?" can diverge arbitrarily — a
model can be highly confident about a recommendation for a case with poor
recovery odds (e.g. a third failed attempt on a stolen/expired card), or
uncertain about a recommendation that is nonetheless statistically likely
to succeed (e.g. a common transient gateway blip). Conflating the two
would let the model's self-assessment silently stand in for an actual
probability estimate, with no way to later replace it with a real
calibrated model without also relitigating what `Confidence` means
everywhere else it's already used (Milestone 3's audit trail, API
responses, etc.). Keeping them structurally separate means Milestone 4
(and any future probability model) can evolve independently of
Milestone 3's AI contract.

**Why a heuristic and not something more sophisticated:** RevGuard has no
historical recovery outcome data to calibrate a real model against yet
(`RecoveryOutcome`, defined in Milestone 1, is not populated by anything
through Milestone 4 — nothing executes actions or observes outcomes yet).
Building or claiming a statistical/ML model without that data would be
fabricating rigor RevGuard doesn't have. A transparent, documented,
versioned heuristic is honest about what it is, is fully unit-testable
and deterministic, and gives the Economic Engine a real (if illustrative)
number to work with today. The `RecoveryProbabilityEstimator` interface
boundary means swapping in a calibrated model later is a new
implementation, not an architecture change.

**Why the engine doesn't decide:** the project's core principle — "AI
recommends. Economic Engine evaluates. Policy decides. Infrastructure
executes. Webhooks verify. Analytics proves." — assigns each concern to
exactly one layer. Policy requires business judgment this milestone has
no basis for: risk appetite, threshold tuning, human-approval rules, and
how confidence/risk-flags should factor into an ALLOW/BLOCK/ESCALATE
decision. Building that now, without Milestone 5's actual requirements,
would mean guessing at policy and rebuilding it later. Keeping
`RecoveryCase.Status` at `ANALYZED` after evaluation also means this
milestone is safe to run against production-shaped data with zero risk of
triggering any action — it only ever adds a row and an audit event.

## Consequences

- `RecoveryEconomicEvaluation` and `RecoveryDiagnosis.Confidence` will
  sometimes look like they're telling contradictory stories (e.g. high
  confidence, low probability) for the same case. This is intentional and
  expected — see "Why separate probability from confidence" above. Do not
  "fix" this by making them agree.
- Milestone 5 has clean, versioned inputs to build policy on
  (`RecoveryEconomicEvaluation.ExpectedIncrementalValueMinorUnits`,
  `RecoveryProbabilityBps`, plus the underlying `RecoveryDiagnosis` and
  its `Confidence`) without needing to touch the Economic Engine itself.
- The heuristic's coefficients will need real recalibration once RevGuard
  has recovery outcome data. Until then, every evaluation is honestly
  labeled `estimator_name="heuristic"`, `estimator_version="heuristic-v1"`
  so nothing downstream can mistake it for a calibrated result.
