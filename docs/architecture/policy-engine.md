# Policy & Safety Engine — Milestone 5

This document describes how RevGuard decides whether a diagnosed,
economically-evaluated recommendation is authorized to proceed. It covers
only what Milestone 5 implements: deterministic policy decisions.
Execution (actually doing the authorized action) is Milestone 6 and is
not described here beyond its integration point.

**The central rule: policy authorization ≠ execution.** An `ALLOW`
decision means the recommendation is authorized to proceed to a future
execution milestone — nothing has happened yet. No `RecoveryAction` is
created by this milestone, no payment gateway is called, no message is
sent to a customer.

## Purpose

By the end of Milestone 4, a `RecoveryCase` has a `RecoveryDiagnosis`
(what the AI recommends) and a `RecoveryEconomicEvaluation` (whether that
recommendation looks economically worthwhile). Neither of those, alone or
together, is authorization. The Policy Engine is the single place that
converts "here's a recommendation with a confidence score and an economic
case" into "yes/no/ask a human" — deterministically, auditably, and
without ever touching a payment.

## Inputs

The Policy Engine reads only data that already exists in the domain
model — nothing invented for this milestone:

- `RecoveryCase.Status` (must be `ANALYZED`) and `PaymentID`
- `RecoveryDiagnosis.RecommendedAction` and `.Confidence`
- `RecoveryEconomicEvaluation.RevenueAtRisk` and
  `.ExpectedIncrementalValueMinorUnits`
- Payment attempt count (`PaymentAttemptRepository.ListByPaymentID`,
  established in Milestone 3)
- Prior recovery action count
  (`RecoveryActionRepository.ListByRecoveryCaseID`, established in
  Milestone 3) — note this counts `RecoveryAction` rows, which nothing in
  the codebase creates yet (execution is Milestone 6), so this count is
  currently always zero in practice; the rule exists and is fully tested
  for when that changes.

No customer consent fields, no payment-method-capability fields, no risk
fields beyond what `RecoveryDiagnosis`/`RecoveryEconomicEvaluation`
already carry — those don't exist in the domain model, and this milestone
does not invent them.

## Why AI confidence is not authorization

`RecoveryDiagnosis.Confidence` answers "how confident is the AI in this
recommendation?" (Milestone 3). The Policy Engine uses it as **one input
among several** (rule C, below) — never as the sole or final answer. A
recommendation is never authorized *because* confidence is high; it can
only be authorized by passing every applicable rule, of which the
confidence check is one. See
[`docs/decisions/0001-economic-engine-probability-vs-confidence.md`](../decisions/0001-economic-engine-probability-vs-confidence.md)
for the parallel reasoning already established in Milestone 4 (recovery
probability vs. confidence), and
[`docs/decisions/0002-three-layer-separation.md`](../decisions/0002-three-layer-separation.md)
for why this separation exists as a structural, not just documentary,
guarantee.

## Why economic value is not authorization

`RecoveryEconomicEvaluation.ExpectedIncrementalValueMinorUnits` being
positive means the numbers look favorable — it says nothing about
confidence, revenue-at-risk size, attempt history, or whether the
recommended action is one RevGuard permits to run automatically at all. A
₹10,000,000 payment with a strongly positive expected value still
requires human review in the default policy (rule E) — size alone
matters independently of value. Economic evaluation answers "is this
worth doing," policy answers "are we allowed to do it automatically" —
two different questions.

## Why policy is deterministic

Every rule is a fixed comparison against a versioned, illustrative
threshold — no model call, no learned weights, no randomness. The same
inputs always produce the same decision, which is the entire point: an
authorization boundary that a malformed, malicious, or merely overconfident
upstream recommendation cannot talk its way past. See
`backend/internal/service/policy_rules.go`'s `evaluatePolicyRules`, a
pure function with no I/O, unit-tested directly.

## Rule evaluation order and decision semantics

`PolicyConfig` (`backend/internal/service/policy_config.go`,
`DefaultPolicyConfig`, version `policy-v1`) holds every threshold:

| Field | Meaning |
|---|---|
| `MinimumConfidence` | floor on `RecoveryDiagnosis.Confidence` (float64 — Milestone 3's existing type, not redesigned) |
| `MaxAutoAmountMinorUnits` | ceiling on `RevenueAtRisk` for automatic authorization — see "One threshold, two names" below |
| `MinimumExpectedIncrementalValueMinorUnits` | floor on expected incremental value |
| `MaxPaymentAttempts` | ceiling on payment attempt count |
| `MaxPriorRecoveryActions` | ceiling on prior recovery action count |
| `AutoAllowedActions` | which `RecommendedAction` values may proceed automatically at all |

`evaluatePolicyRules` checks every rule (it does not short-circuit on the
first match) and collects **every** reason code that applies, not just
the first or most severe. The final `Outcome` is then decided by
severity, not evaluation order: **BLOCK outranks ESCALATE, which outranks
ALLOW**. `ALLOW` only happens when zero rules fire.

Rules, in the order they're checked (severity, not priority — see above):

| # | Condition | Outcome | Reason code |
|---|---|---|---|
| B | `RecommendedAction == stop_recovery` | BLOCK | `STOP_RECOVERY_RECOMMENDATION` |
| C | `Confidence < MinimumConfidence` | ESCALATE | `LOW_AI_CONFIDENCE` |
| D | `ExpectedIncrementalValue < MinimumExpectedIncrementalValue` | BLOCK | `NEGATIVE_EXPECTED_VALUE` |
| E | `RevenueAtRisk > MaxAutoAmountMinorUnits` | ESCALATE | `AMOUNT_ABOVE_AUTO_LIMIT` |
| F | `PaymentAttemptCount >= MaxPaymentAttempts` | BLOCK | `MAX_ATTEMPTS_REACHED` |
| G | `PriorRecoveryActionCount >= MaxPriorRecoveryActions` | ESCALATE | `TOO_MANY_PRIOR_ACTIONS` |
| H | `!AutoAllowedActions[RecommendedAction]` (skipped for `stop_recovery`, already covered by B) | ESCALATE | `ACTION_NOT_AUTO_ALLOWED` |
| — | none of the above fired | ALLOW | `POLICY_ALLOWED` |

All comparisons are exact integer/float comparisons (`<`, `<=`, `>`,
`>=`) — never approximate. Every boundary (exactly-at-threshold vs.
one-unit-past-threshold) is unit tested in `policy_rules_test.go`.

### One threshold, two names

The milestone brief lists both "maximum automatic recovery amount" and a
separate "human approval threshold" as configuration knobs, and rules E
and I as separate checks. In this codebase's smallest coherent model,
these are **the same question** — "is this amount too large to
auto-authorize?" — so `MaxAutoAmountMinorUnits` serves both purposes and
rule I is folded into rule E rather than duplicated. There is no data or
domain concept in the current model to justify two independent amount
thresholds; inventing one would be exactly the kind of fabricated
distinction this milestone was told to avoid. See
`PolicyConfig.MaxAutoAmountMinorUnits`'s doc comment.

### Why rule G escalates rather than blocks

Unlike rule F (raw payment attempts — a blunt, hard signal that the
payment method itself keeps failing), too many *recovery actions* having
already been tried (rule G) suggests the case needs a human decision
about strategy, not necessarily that recovery is hopeless. This is a
judgment call, documented here and in `policy_rules.go`'s inline comment,
not an arbitrary default — see `AutoAllowedActions` and Definition of
Done item G's "ESCALATE or BLOCK according to documented policy."

## Decision model

`domain.PolicyDecision` (`backend/internal/domain/policy_decision.go`):
`Outcome` (`domain.PolicyDecisionOutcome`: `ALLOW`/`BLOCK`/`ESCALATE`),
`AuthorizedAction` (set only when `Outcome == ALLOW` — empty for
BLOCK/ESCALATE, since nothing is authorized in those cases),
`PolicyVersion`, `ReasonCodes` (`[]domain.PolicyReasonCode`, may contain
more than one), `Explanation` (a full human-readable summary of every
input and threshold, not just the triggered ones), `EvaluatedAt`,
`CreatedAt`, plus the exact `RecoveryDiagnosisID` and
`RecoveryEconomicEvaluationID` it evaluated.

`PolicyDecisionOutcome`'s three values are deliberately the same strings
as the corresponding `RecoveryCaseStatus` values — see that type's doc
comment for why this is safe here (unlike `RecommendedAction` vs.
`RecoveryActionType` in Milestones 3–4, there's no risk of confusing a
suggestion with an authorization: the outcome *is* the authorization).

## Database

Migration `000014` adds `policy_decisions` — a new table; no Milestone
1–4 migration was modified. `UNIQUE(recovery_case_id,
recovery_diagnosis_id, recovery_economic_evaluation_id, policy_version)`
is the idempotency guarantee: the exact same tuple can never produce two
decisions. A new diagnosis, a new economic evaluation (both from
re-analysis), or a new policy version legitimately produces a new,
independent decision. Rows are never updated after creation.

## Policy Engine service

`PolicyEngine.Evaluate(ctx, recoveryCaseID, recoveryDiagnosisID,
recoveryEconomicEvaluationID)`
(`backend/internal/service/policy_engine.go`):

1. Checks for an existing decision for the exact input tuple first
   (idempotency), before validating anything else — a prior successful
   call already did that validation, and the case's *current* status
   (which might now be `ALLOW`/`BLOCK`/`ESCALATE`) is a legitimate
   consequence of that prior decision, not an error condition. Checking
   "is the case ANALYZED?" before this idempotency check would wrongly
   reject a safe retry.
2. If no existing decision: loads the case, diagnosis, and economic
   evaluation, validating existence and ownership
   (`ErrDiagnosisCaseMismatch`, `ErrEconomicEvaluationCaseMismatch`,
   `ErrEconomicEvaluationDiagnosisMismatch`), then requires
   `RecoveryCase.Status == ANALYZED` (`ErrRecoveryCaseNotAnalyzed`).
3. Loads payment attempt and prior-recovery-action counts.
4. Calls `evaluatePolicyRules` (pure, no I/O).
5. Persists the `PolicyDecision`
   (`INSERT ... ON CONFLICT (...) DO NOTHING`, same idempotent-insert
   pattern as `RecoveryEconomicEvaluationRepository.TryCreate` —
   Milestone 4).
6. Transitions the case `ANALYZED -> POLICY_CHECK -> {ALLOW, BLOCK,
   ESCALATE}` using the existing `ValidateTransition`/`UpdateStatus`
   guarded-update pattern from Milestone 2.
7. Writes an `AuditEvent`.
8. Commits.

**Never**: calls the AI service, calls a payment gateway, creates a
`RecoveryAction`, transitions past `ALLOW`/`BLOCK`/`ESCALATE` (no
`EXECUTING`, no `CLOSED`).

## State transitions

The state machine already declared
`ANALYZED -> POLICY_CHECK -> {ALLOW, BLOCK, ESCALATE}` since Milestone 2
(`allowedTransitions` in `state_machine.go`) — no state-machine change was
needed for this milestone. `Evaluate` performs both hops
(`ANALYZED -> POLICY_CHECK`, then `POLICY_CHECK -> <outcome>`) inside the
same transaction: nothing outside this function ever observes a case
sitting in `POLICY_CHECK`, since there is no external call between the
two hops to make that intermediate state externally meaningful (compare
with `AnalyzeCase`'s `ANALYZING`, which genuinely persists across an AI
HTTP call). `ALLOW -> EXECUTING`, `BLOCK -> CLOSED`, and
`ESCALATE -> <anything>` are explicitly not implemented — those are later
workflow milestones (`ESCALATE` still has no outgoing edge in the state
machine, exactly as before this milestone).

## Transaction boundary

The Policy Engine makes no external network call (no AI service, no
payment gateway — nothing). Like `EconomicEngine` (Milestone 4) and
unlike `AnalysisOrchestrator`'s AI call (Milestone 3), `Evaluate` does
all of its work — reads and writes — inside **one** short PostgreSQL
transaction, using the existing `repository.DBTX` abstraction so the same
repository code runs against either the pool or the transaction.

## Idempotency and concurrency

`PolicyDecisionRepository.TryCreate` uses
`INSERT ... ON CONFLICT (...) DO NOTHING`, which never raises an error on
conflict and therefore never poisons the enclosing transaction — the
loser of a race can safely re-query for the winner's row in the same
transaction, no `SAVEPOINT` needed (contrast with Milestone 2's
case-creation race, which does need one because it uses a plain `INSERT`
that can error). PostgreSQL's unique constraint is the sole durable
authority; no Redis lock is used or required.
`TestPolicyEngine_ConcurrentEvaluationConvergesSafely` drives 5 concurrent
goroutines at the identical input tuple and asserts exactly one decision
row and exactly one `Created=true` result — see the test's log output in
`CLAUDE.md`'s Milestone 5 verification notes for a real observed run.

## Auditability

Every newly created decision writes one `AuditEvent`
(`event_type = "recovery_policy.evaluated"`, `actor_type = SYSTEM`) with
metadata covering the diagnosis ID, economic evaluation ID, decision,
authorized action, policy version, reason codes, revenue at risk, and
expected incremental value — enough to answer "what did RevGuard decide,
why, using which diagnosis, which evaluation, and which policy version"
without a second query. An idempotent no-op does not write a second audit
row. `PolicyDecision.Explanation` additionally records every threshold
compared against (not just the ones that triggered), so a decision is
fully reconstructable from the row alone.

## Orchestration integration and failure behavior

`EventProcessor.Process` calls `PolicyEvaluator.Evaluate` (an interface
satisfied by `*PolicyEngine`) immediately after a successful economic
evaluation (`result.EconomicEvaluation != nil`) — the fourth step in the
same after-commit chain established in Milestones 3–4
(analysis → economic evaluation → policy evaluation). `ProcessResult`
gained `PolicyDecision`/`PolicyEvaluationError` fields, mirroring the
`Diagnosis`/`AnalysisError` and
`EconomicEvaluation`/`EconomicEvaluationError` pattern.

**Chosen failure behavior**: if policy evaluation fails (a Go error from
`Evaluate` — e.g. a mismatch, or a database error), `EventProcessor` does
not fail the overall `POST /events` request: the event, case, diagnosis,
and economic evaluation are already durably recorded regardless. The
error is logged and surfaced separately via `PolicyEvaluationError`. The
case is left at `ANALYZED` — `Evaluate` never partially transitions it
(the two `UpdateStatus` calls only happen after every validation and the
decision insert have already succeeded). This is never silently
fabricated as a decision: a failed evaluation produces **no**
`PolicyDecision` row at all, not a default-BLOCK or default-ESCALATE
placeholder.

## Read endpoint

`GET /v1/recovery-cases/{id}/policy-decision`
(`backend/internal/http/policy_decision.go`) returns the latest decision
for a case. Strictly read-only — no approve/override/execute capability
exists at this or any endpoint.

## Milestone 10: policy profiles

A merchant's risk appetite varies. Milestone 10 added three named
`PolicyConfig` values (`policy_config.go`) expressing that as an explicit
choice rather than one hardcoded set of numbers — **the rules
(`evaluatePolicyRules`) are byte-for-byte unchanged**; only the threshold
*values* a profile supplies differ:

| Threshold | Conservative | Balanced (= original default) | Aggressive |
|---|---|---|---|
| `Version` | `policy-v1-conservative` | `policy-v1` | `policy-v1-aggressive` |
| `MinimumConfidence` | 0.75 | 0.60 | 0.50 |
| `MaxAutoAmountMinorUnits` | 50,000 (₹500) | 100,000 (₹1,000) | 300,000 (₹3,000) |
| `MinimumExpectedIncrementalValueMinorUnits` | 500 | 0 | 0 |
| `MaxPaymentAttempts` | 2 | 3 | 4 |
| `MaxPriorRecoveryActions` | 1 | 2 | 3 |
| `AutoAllowedActions` extra | — | — | `request_payment_method_change: true` |

These are RevGuard's own illustrative demonstration configurations —
not claimed production Razorpay policy, not derived from historical loss
data, exactly like the original default policy they extend.

**Safety invariants that hold identically across every profile, because
they live in code, not config:**

- `stop_recovery` is unconditionally `BLOCK`ed by rule (B) regardless of
  any profile's `AutoAllowedActions` map — no profile's config can
  re-enable it, because the check isn't config-driven.
- `MinimumExpectedIncrementalValueMinorUnits` is never negative in any
  profile: "aggressive" means more tolerant thresholds elsewhere, never
  authorization of a computed negative-expected-value action.
- No profile lets confidence alone or expected value alone authorize —
  both remain two independent checks among seven, exactly as in the
  original design (see "Deterministic rules" above).

`BalancedPolicyConfig` and the original `DefaultPolicyConfig` are two
separate Go variables with identical values (not one aliasing the
other), specifically so nothing about `cmd/server/main.go`'s existing
wiring or any pre-Milestone-10 test has to change — see
`TestBalancedPolicyConfig_MatchesDefaultPolicyConfig`.

**Selecting a profile in production:** `POLICY_PROFILE` (env var,
default `"balanced"`) is read in `cmd/server/main.go` and looked up in
`service.PolicyProfiles`; an unrecognized value fails fast at startup
rather than silently defaulting. This is the only new production
(non-evaluation) capability Milestone 10 added — nothing about how
`PolicyEngine.Evaluate` itself works changed; it still takes a
`PolicyConfig` as a constructor parameter exactly as it always has.

**Selecting a profile in the evaluation harness:** see
`docs/architecture/evaluation-engine.md`'s Milestone 10 section —
`RunEvaluation` now runs all three profiles against the identical
synthetic dataset and reports the trade-off between them directly.

## What is explicitly out of scope (by design)

Execution of any kind, Razorpay API calls, payment retries/links as real
side effects, customer communication, webhooks, reconciliation, Redis-based
financial state, a policy admin UI, machine learning, and any transition
beyond `ALLOW`/`BLOCK`/`ESCALATE`. `PolicyEngine` has no code path toward
any of these — see `docs/decisions/0002-three-layer-separation.md`.
