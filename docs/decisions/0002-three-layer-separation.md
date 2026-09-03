# ADR 0002: AI recommendation, economic evaluation, and policy authorization are three separate layers

- Status: Accepted
- Date: 2026-09-03
- Milestone: 5 — Policy & Safety

## Context

By Milestone 4, a `RecoveryCase` accumulates two pieces of machine-produced
output: a `RecoveryDiagnosis` (Milestone 3, from the Python AI service —
what to do, and how confident the model is) and a
`RecoveryEconomicEvaluation` (Milestone 4, from a deterministic Go
heuristic — whether that recommendation looks economically worthwhile).
Milestone 5 needed to decide whether to actually authorize the
recommendation to proceed toward execution.

The fast path would have been to fold that decision into whichever
component already had the relevant number: gate on
`RecoveryDiagnosis.Confidence` directly, or gate on
`RecoveryEconomicEvaluation.ExpectedIncrementalValueMinorUnits` being
positive, inside `AnalysisOrchestrator` or `EconomicEngine` respectively.

We rejected that shortcut and built a third, independent layer instead:
`PolicyEngine`, evaluating a fixed, versioned rule set that treats
confidence and economic value as two inputs among several — never as the
decision itself.

## Decision

RevGuard's recovery pipeline is now four structurally separate layers,
each with a distinct authority boundary:

```
AI recommends           (Python ai-service, Milestone 3)
    ↓
Economic Engine evaluates value   (Go, deterministic, Milestone 4)
    ↓
Policy Engine decides authority   (Go, deterministic, Milestone 5)
    ↓
Execution Engine performs action  (Go, Milestone 6 — not yet built)
```

Each layer's output is a durable, versioned, auditable record
(`RecoveryDiagnosis`, `RecoveryEconomicEvaluation`, `PolicyDecision`) that
the next layer reads but does not blindly trust: every cross-layer
reference is validated (diagnosis belongs to the case; evaluation belongs
to both the case and the specific diagnosis it was computed from) rather
than assumed correct because it came from "our own" prior step.

## Rationale

**A single AI-confidence gate would let the model authorize itself.** If
`if diagnosis.Confidence > threshold { allow }` were the whole policy, a
sufficiently (over)confident model output would be the only thing standing
between a recommendation and authorization. The Policy Engine's rule for
confidence (`LOW_AI_CONFIDENCE`) is one of seven independent checks, and
failing all the *other* checks still blocks or escalates regardless of how
confident the model was. See
`backend/internal/service/policy_rules.go`'s package doc comment.

**A single positive-expected-value gate ignores everything policy cares
about that economics doesn't model.** `EconomicEngine` (Milestone 4)
deliberately has no concept of "is this action allowed to run
automatically," "is this amount too large for auto-authorization without
a human," or "has this case already had too many recovery attempts." Those
are policy questions, not economic ones — bolting them onto
`EconomicEngine` would have made it responsible for concerns it has no
data model for, and would have meant Milestone 4's formulas needed
revisiting every time a policy threshold changed. Keeping them in
`PolicyEngine` means `EconomicEngine`'s formulas stay exactly as
Milestone 4 defined them.

**Determinism is the actual safety property.** `PolicyEngine.Evaluate`'s
core rule logic (`evaluatePolicyRules`) is a pure function: same inputs,
same output, every time, with no model call and no learned weights. This
is what makes "a malicious, malformed, overconfident, or economically
attractive AI recommendation must never bypass deterministic Go policy" a
provable property of the code rather than an aspiration: there is no
code path from Python's output to an `ALLOW` decision that does not pass
through this fixed rule set.

**Each layer stopping exactly where it should is what keeps the whole
pipeline auditable.** `RecoveryDiagnosis` never mutates durable case
state. `RecoveryEconomicEvaluation` never mutates durable case state.
`PolicyDecision` transitions the case status but never executes anything
and never creates a `RecoveryAction`. Every layer's boundary is enforced
in code (see each service's package doc comment) and verified in tests
(e.g. `TestPolicyEngine_*` asserting zero `recovery_actions` rows after
every decision) — not just documented as a convention.

## Consequences

- Milestone 6 (execution) has a clean, well-defined starting point: an
  `ALLOW` `PolicyDecision` referencing an exact `RecoveryDiagnosis` (which
  action) and `RecoveryEconomicEvaluation` (why it's worth it). It does
  not need to re-derive authorization logic or re-read AI confidence —
  `PolicyDecision.AuthorizedAction` is the single field it needs.
- Adding a new policy rule, or changing a threshold, never requires
  touching `ai-service/` or `EconomicEngine`. Conversely, recalibrating
  the economic model (e.g. once real historical outcome data exists —
  see ADR 0001) never requires touching policy thresholds.
- Every decision is reproducible after the fact: given a `PolicyDecision`
  row, its `RecoveryDiagnosisID` and `RecoveryEconomicEvaluationID`
  reference the exact inputs, and its `PolicyVersion` identifies the
  exact rule set — nothing about "what would today's policy engine say
  about a two-week-old case's diagnosis" is ambiguous.
- This does mean three round-trips through the domain model
  (diagnosis → evaluation → decision) instead of one combined step. That
  cost is intentional: collapsing the layers to save a database round
  trip would reintroduce exactly the coupling this ADR exists to prevent.
