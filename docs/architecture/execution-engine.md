# Execution Engine — Milestone 6 (execution coverage extended in Milestone 10)

This document describes how RevGuard turns an `ALLOW` `PolicyDecision`
into a bounded, auditable execution attempt against a payment provider.
Milestone 6 implemented `retry_payment`; Milestone 10 added
`send_payment_link` through the identical mechanism (see "Milestone 10:
send_payment_link" below) — both are recorded honestly, including when
the result is genuinely unknown. Financial truth (webhook verification,
reconciliation, `SUCCESS`/`FAILED` as a durable, trusted state) is
Milestone 7 and is not established here.

**The central rule: the persisted `PolicyDecision` is the only
authorization the Execution Engine trusts.** No HTTP request parameter,
AI recommendation, or client-supplied action can select what gets
executed. See "Why the policy decision is authoritative" below.

## Purpose

By the end of Milestone 5, a `RecoveryCase` that reaches `ALLOW` has a
durable `PolicyDecision` row naming exactly one `AuthorizedAction`.
Nothing has executed yet — `ALLOW` means "authorized to proceed," not
"done." The Execution Engine is the one place that turns that
authorization into a real attempt: creating a `RecoveryAction` record,
calling a `PaymentProvider`, and recording — durably and honestly —
whatever the provider actually said, including "I don't know."

## Why the policy decision is authoritative

`ExecutionEngine.Execute(ctx, recoveryCaseID, policyDecisionID)` takes a
`policyDecisionID`, never an action. The only action ever executed is
`PolicyDecision.AuthorizedAction`, reloaded fresh from PostgreSQL inside
`Execute`'s own transaction for the exact `policyDecisionID` given — never
a value read from an HTTP body, query string, or the AI's own
recommendation. If an attacker (or a bug) requests execution with a
`PolicyDecision` that is `BLOCK` or `ESCALATE`, or asks for a *different*
action than the one that was actually authorized, there is no code path
in `ExecutionEngine` that can honor that request — see the validation
chain below. This is the same three-layer separation Milestone 5 built
(AI recommends, policy decides) with one more layer added: infrastructure
executes only what policy decided, never what was asked for.

`POST /v1/recovery-cases/{id}/execute` reflects this at the HTTP
boundary: the request body is empty. The handler resolves the latest
policy decision for the case server-side
(`policyDecisionReader.GetLatestDecision`, the same interface Milestone
5's read endpoint already used) and passes its ID into `Execute` — there
is no `action` field anywhere in the request for a client to set.

## Validation chain

Any failure below returns a typed error and produces **no** execution
side effect — no `RecoveryAction` row, no provider call:

1. The `PolicyDecision` exists (`ErrPolicyDecisionNotFound`).
2. It belongs to the requested `recoveryCaseID`
   (`ErrPolicyDecisionCaseMismatch`).
3. Its `Outcome` is `ALLOW` (`ErrPolicyDecisionNotAllow`) — `BLOCK` and
   `ESCALATE` can never reach execution, structurally, not just by
   convention.
4. It has a non-empty, valid `AuthorizedAction`
   (`ErrMissingAuthorizedAction`) — this should be structurally
   impossible given `PolicyEngine`'s own invariants (Milestone 5), but
   `ExecutionEngine` checks it explicitly rather than trusting that
   invariant blindly.
5. That action has a real execution implementation
   (`ErrActionNotExecutable`) — Milestone 6 implements only
   `retry_payment`. `send_payment_link`,
   `request_payment_method_change`, `send_reminder`,
   `escalate_to_human`, and `stop_recovery` are recognized,
   structurally valid `RecommendedAction` values that policy can and
   does authorize (see the Milestone 5 manual verification, which
   observed a real `ALLOW` for `send_payment_link`), but none of them
   fabricates a fake financial side effect here — an unsupported action
   is reported as a clear, typed rejection, never silently treated as
   "done."
6. The `RecoveryCase` is currently `ALLOW`
   (`ErrRecoveryCaseNotAllow`) — `ANALYZED`, `DETECTED`, `BLOCK`,
   `ESCALATE`, `EXECUTING`, `VERIFYING`, `SUCCESS`, `FAILED`, `UNKNOWN`,
   and `CLOSED` are all rejected. Combined with step 3, this means a
   case can only ever be pushed into `EXECUTING` from `ALLOW`, and only
   by way of a decision that really is `ALLOW`.

## Transaction boundary: three phases, one external call

The forbidden pattern is holding a database transaction open across a
network call to a payment provider — a slow or hanging gateway would then
hold Postgres connections and locks hostage. `Execute` follows the same
two-phase-plus-result structure `AnalysisOrchestrator` established for
the AI service call in Milestone 3, split into three named phases:

- **Phase 1** (`phase1`, one short transaction): validates the chain
  above, creates the `RecoveryAction` row (`status = EXECUTING`),
  transitions `ALLOW -> EXECUTING`, writes a
  `recovery_execution.started` audit event, commits. No provider has
  been called yet.
- **Phase 2** (outside any transaction): calls
  `PaymentProvider.RetryPayment`. No database connection or lock is held
  during this call.
- **Phase 3** (`phase3`, one short transaction): persists the provider's
  result on the `RecoveryAction` row, transitions
  `EXECUTING -> VERIFYING`, writes a `recovery_execution.completed` (or
  `.unknown`) audit event, commits.

If Phase 1 determines no provider call is needed (an idempotent retry —
see below), `Execute` returns immediately after Phase 1 and Phase 2/3
never run.

## Idempotency

`domain.RecoveryAction.IdempotencyKey` (a `UNIQUE` column since
Milestone 1, reused rather than redesigned) is set deterministically to
`"policy-decision:" + policyDecisionID.String()`. Since a `PolicyDecision`
is itself immutable and uniquely tied to one `(case, diagnosis,
evaluation, policy version)` tuple (Milestone 5), this guarantees **at
most one `RecoveryAction` row is ever created per policy decision**, and
therefore per logical execution — repeated HTTP retries of
`POST /execute`, a crashed process restarted, or concurrent callers all
converge on the same row.

`RecoveryActionRepository.TryCreate` uses
`INSERT ... ON CONFLICT (idempotency_key) DO NOTHING` (the same pattern
as `RecoveryEconomicEvaluationRepository.TryCreate` and
`PolicyDecisionRepository.TryCreate` from Milestones 4–5) — it never
raises an error on conflict, so the loser of a race can safely re-query
for the winner's row in the same transaction without a `SAVEPOINT`.

When `phase1` finds an existing `RecoveryAction` for the idempotency key
(`resumeExisting`), it classifies it three ways:

- **Terminal** (`SUCCEEDED`, `FAILED`, or `UNKNOWN`): a genuine repeat —
  no-op, return the existing result, never call the provider again.
- **`EXECUTING` and recent** (younger than `executionStaleAfter`, 30
  seconds): plausibly still genuinely in flight — another call or
  process is between Phase 1 and Phase 3 *right now*. No-op, touch
  nothing, never call the provider ourselves. This is what makes the
  concurrency test safe: five goroutines racing the same policy decision
  converge on exactly one provider call, with the four losers simply
  reporting the winner's in-progress state rather than either blocking
  indefinitely or calling the provider redundantly.
- **`EXECUTING` and stale** (older than `executionStaleAfter`): the
  process that started this attempt is assumed to have crashed or been
  killed between Phase 1 and Phase 3. We cannot know whether it ever
  reached the provider, so — critically — **we do not call the provider
  again**. The row is resolved to `UNKNOWN`
  (`error_code = EXECUTION_STALE_AMBIGUOUS`) and the case is moved to
  `VERIFYING`, exactly like a real ambiguous provider response. This is
  the same "never guess, never blindly retry a payment operation"
  principle applied to a different source of ambiguity (an abandoned
  process instead of a network timeout).

`phase1` also re-checks the idempotency key immediately before rejecting
a wrong-state case (mirroring a real race condition found and fixed in
Milestone 5's `PolicyEngine`, see below) — under PostgreSQL's READ
COMMITTED isolation, each `SELECT` in a transaction sees a fresh
snapshot, so a concurrent `Execute` call for the same `policyDecisionID`
can fully commit in the gap between the idempotency check and the
case-status check. Re-checking idempotency once more before erroring
turns what would otherwise be a spurious `ErrRecoveryCaseNotAllow` into a
correct, safe no-op.

## Timeout / ambiguous outcome semantics — `UNKNOWN`

This is the most important correctness property in this milestone.
`PaymentProvider.RetryPayment` returns `(RetryPaymentResult, nil)` for
any **definitive** outcome — success or failure, either of which the
provider clearly reported — and a non-nil `error` for anything
**ambiguous**: a request timeout, a transport/connection failure, or any
other condition where RevGuard cannot be sure whether the provider-side
operation actually happened. `phase3`'s classification is a plain
`switch`:

```go
switch {
case providerErr != nil:
        newStatus = domain.RecoveryActionStatusUnknown   // never guessed into SUCCEEDED/FAILED
case result.Succeeded:
        newStatus = domain.RecoveryActionStatusSucceeded
default:
        newStatus = domain.RecoveryActionStatusFailed     // provider explicitly declined/rejected
}
```

A non-nil `error` is never retried automatically and never mapped to
`FAILED` — a provider might have fully processed a retry and merely
failed to deliver the response before the timeout, and reporting that as
`FAILED` risks a spurious second attempt at reconciliation time, while
reporting it as `SUCCEEDED` risks masking a real failure. `UNKNOWN` is the
only honest answer. `RecoveryCaseStatusUnknown` was already declared in
the state machine (Milestone 1/2 vocabulary) but had no code path
producing it until now.

Whichever outcome fires, the case still transitions
`EXECUTING -> VERIFYING` — including for `UNKNOWN`. The case is never
left in `EXECUTING` (which would look like it's still mid-flight
forever) and never advanced past `VERIFYING` to `SUCCESS` or `FAILED` by
this engine. `VERIFYING` is where the case waits for Milestone 7's
webhook/reconciliation to establish financial truth — this milestone
deliberately stops there. The audit event type itself makes the
ambiguity explicit: `recovery_execution.unknown` (vs.
`recovery_execution.completed` for a definitive success or failure),
with metadata recording `reason: "provider_response_ambiguous"` (a real
provider error) or `reason: "execution_stale_no_result_recorded"` (an
abandoned attempt resolved without ever calling the provider).

## Provider abstraction

```go
type PaymentProvider interface {
        Name() string
        RetryPayment(ctx context.Context, request RetryPaymentRequest) (RetryPaymentResult, error)
}
```

(`backend/internal/service/payment_provider.go`) — `ExecutionEngine`
depends on this interface only; it has no HTTP/transport code of its
own, mirroring how `AIClient` is the only thing in the codebase that
knows how to reach the Python service. `RetryPaymentRequest` carries only
an idempotency key, the external payment ID, amount, and currency — no
card data, CVV, or credentials, because `domain.Payment` doesn't model
those fields (the same "nothing to leak" guarantee
`RecoveryContextBuilder` relies on at the AI boundary).

### `FakeProvider` (`fake_payment_provider.go`)

A deterministic, in-process provider for tests and local development.
Always identifies itself as `provider = "fake"` — persisted
`RecoveryAction.Provider` values can never be mistaken for a real
execution. Five scenarios, selected at construction:

| Scenario | Result |
|---|---|
| `success` | Definitive success, `ProviderReference = "fake_ref_<idempotency_key>"` |
| `definitive_failure` | Definitive failure, `ErrorCode = "CARD_DECLINED"` |
| `unsupported` | Definitive failure, `ErrorCode = "UNSUPPORTED_OPERATION"` — the provider understood the request and gave a clear answer; still not ambiguous |
| `timeout` | Ambiguous (`ErrFakeProviderTimeout`) |
| `transport_error` | Ambiguous (`ErrFakeProviderTransportError`) |

`InvocationCount()` is atomic, letting concurrency/idempotency tests
assert exactly how many times the provider actually ran (never more than
once per policy decision, regardless of how many callers raced or
retried).

### `RazorpayProvider` (`razorpay_provider.go`) — honestly scoped

**What "retry" means here, and why it isn't literal.** Razorpay's public
API has no server-to-server "force retry this failed card payment"
operation, and — independent of Razorpay specifically — Indian payment
regulation (RBI-mandated additional-factor authentication) requires the
customer to re-authenticate for each card charge. A backend cannot
silently re-charge a card without the customer present. So
`retry_payment` cannot be mapped to a literal "do it again" call without
inventing an API Razorpay doesn't expose. The closest safe, real,
well-documented operation is creating a **Payment Link**
(`POST /v1/payment_links`) — a hosted checkout page handed back to the
customer to complete payment again. A `Succeeded` result from this
provider means "a retry mechanism was successfully created and handed to
the customer," not "the payment succeeded." Final payment truth is only
ever established by Milestone 7's webhook reconciliation, exactly like
every other provider in this codebase.

Credentials (`RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`) come from
environment variables only, read by `cmd/server/main.go` and passed in
explicitly — never hardcoded, never logged, sent via HTTP Basic Auth in
the `Authorization` header (never the URL or body, so they can never leak
into logs that record request URLs). `NewRazorpayProvider` fails fast at
startup if either credential is empty rather than silently no-op'ing.
5xx responses and transport failures are treated as ambiguous (a `Go`
error); 4xx responses are treated as definitive failure (Razorpay
received and rejected the request); 2xx is definitive success.

**Verification status: NOT VERIFIED against a real Razorpay account.**
No `RAZORPAY_KEY_ID`/`RAZORPAY_KEY_SECRET` are configured in this
sandbox, and this adapter has only been written against Razorpay's
long-stable, publicly documented Payment Links request/response shape —
it has not been exercised against a live endpoint, a sandbox test
account, or re-verified against current API documentation in this
session (no network access to Razorpay's docs from this sandbox). Do not
present this provider as tested. Only `FakeProvider` has been exercised
by this codebase's test suite and by the manual smoke tests described
below.

## Database

Migration `000015` (`backend/migrations/000015_add_execution_fields_to_recovery_actions`)
extends the existing `recovery_actions` table (from Milestone 1) rather
than creating a new one — a `RecoveryAction` row already represented
"something RevGuard decided to attempt," and execution results are more
of that same row, not a new concept:

- Extends the `status` `CHECK` constraint with `UNKNOWN` (previously
  `PENDING`/`EXECUTING`/`SUCCEEDED`/`FAILED`/`SKIPPED`).
- Adds `provider TEXT`, `provider_reference TEXT`, `error_code TEXT` —
  all nullable, sanitized, stable values; never a raw provider response
  body.
- Adds `execution_metadata JSONB NOT NULL DEFAULT '{}'` — sanitized
  structured JSON, populated from the same kind of small `map[string]any`
  used for `AuditEvent.Metadata` elsewhere in this codebase, never a raw
  HTTP response dump.
- Adds a partial unique index,
  `idx_recovery_actions_provider_reference_unique ON recovery_actions
  (provider, provider_reference) WHERE provider_reference IS NOT NULL` —
  the same provider can never report the same reference for two
  different actions.

`idempotency_key`'s pre-existing `UNIQUE` constraint (Milestone 1) is
reused as-is for execution idempotency, per the instruction to prefer
extending existing schema over inventing a parallel mechanism.

## Auditability

Every execution attempt writes at least two audit events:
`recovery_execution.started` (Phase 1, `actor_type = SYSTEM`) with the
policy decision ID, the new `RecoveryAction` ID, the authorized action,
and the provider name; and either `recovery_execution.completed` (a
definitive success or failure) or `recovery_execution.unknown` (an
ambiguous result), each recording the action ID, provider, authorized
action, resulting status, provider reference (if any), and error code (if
any). An idempotent no-op (a genuine repeat, or a still-in-flight
concurrent call) writes no additional audit event — only the original
attempt's events exist. No audit metadata ever contains a card number,
CVV, API key, secret, password, or raw `Authorization` header value —
verified directly by
`TestExecutionEngine_NoSecretsInPersistedMetadata`, which scans both
`recovery_actions.execution_metadata` and `audit_events.metadata` for a
list of forbidden substrings after a real execution.

## Orchestration and the HTTP boundary

Unlike Milestones 3–5, `ExecutionEngine` is **not** wired into
`EventProcessor.Process` — execution does not happen automatically the
moment a case reaches `ALLOW`. It is only ever triggered by
`POST /v1/recovery-cases/{id}/execute`
(`backend/internal/http/execution.go`), which:

1. Parses `{id}` as the recovery case ID — the only input the client
   supplies.
2. Resolves the case's latest `PolicyDecision` server-side via
   `policyDecisionReader.GetLatestDecision` (the same lookup Milestone
   5's read endpoint uses).
3. Calls `ExecutionEngine.Execute(ctx, caseID, decision.ID)`.
4. Maps errors: not-found conditions -> 404; `ErrPolicyDecisionNotAllow`,
   `ErrMissingAuthorizedAction`, `ErrActionNotExecutable`,
   `ErrRecoveryCaseNotAllow` -> 422; anything else -> a generic 500
   (never a raw persistence or provider error).
5. On success, returns case ID, action ID, authorized action, provider
   name, execution status, case status, provider reference (safe to
   expose — never a raw response), and an explicit `unknown` boolean —
   never credentials, never a raw provider response body.

There is no "force execute" endpoint, no manual override, and no way for
a request body to name an action — the entire authorization surface is
the server-side policy-decision lookup plus `ExecutionEngine`'s own
validation chain.

## Milestone 10: `send_payment_link`

`send_payment_link` is the second `domain.RecommendedAction` to get a
real execution implementation, added without changing any of the
transaction-boundary, idempotency, or auditability guarantees described
above.

**What changed, precisely:**

- `executableActions` (`execution_engine.go`) replaced the old hardcoded
  `if decision.AuthorizedAction != domain.RecommendedActionRetryPayment`
  check with a map:
  `{retry_payment: RETRY_PAYMENT, send_payment_link: SEND_PAYMENT_LINK}`.
  Extending real execution coverage to a future action is "add an entry
  here, add a case in `Execute`'s provider dispatch, add the matching
  `PaymentProvider` method" — no other engine logic changes.
- `PaymentProvider` gained one new method,
  `SendPaymentLink(ctx, SendPaymentLinkRequest) (SendPaymentLinkResult, error)`,
  with the identical error-vs-result split as `RetryPayment` (a non-nil
  error is always ambiguous, never a definitive failure).
  `FakeProvider.SendPaymentLink` applies the same five deterministic
  scenarios as `RetryPayment`. `RazorpayProvider.SendPaymentLink` calls
  the exact same Payment Links operation as `RetryPayment` — Razorpay's
  API offers no different mechanism for "proactively send a link" versus
  "retry via a link" — via a shared private `createPaymentLink` helper,
  so the HTTP/error-classification logic is never duplicated.
- `Execute`'s Phase 2 now dispatches on `p1.action.ActionType` to call
  either `RetryPayment` or `SendPaymentLink`, then normalizes either
  provider method's result into the same shape before handing it to
  Phase 3 — **Phase 3 itself was not touched.**

**Why this preserves "payment-link creation is not financial success"
automatically, not as a new safety check:** Phase 3 already, since
Milestone 6, transitions `RecoveryCase.Status` unconditionally to
`VERIFYING` (never directly to `SUCCESS`) regardless of the provider's
`Succeeded` value — that was always true for `retry_payment`'s own
"create a Payment Link" implementation (see "Why 'retry' means here" in
the `RazorpayProvider` section above). Wiring `send_payment_link` through
the identical Phase 1/Phase 3 code path means this invariant holds for
it automatically, with zero new logic: a `RecoveryAction` can reach
`SUCCEEDED` (the link was created), while `RecoveryCase` only ever
reaches `VERIFYING`, waiting for Milestone 7's webhook/reconciliation to
establish whether the customer actually paid.

**Tests** (`execution_engine_test.go`,
`TestExecutionEngine_SendPaymentLink_*`): mirror every `retry_payment`
test field-for-field — successful execution (including the explicit
assertion that case status is `VERIFYING`, never `SUCCESS`), definitive
failure, timeout, transport error, idempotency, and 5-goroutine
concurrency — proving `send_payment_link` goes through the identical
pipeline, never a parallel or weaker one.
`seedAllowDecisionForUnsupportedAction` now uses `send_reminder` (still
genuinely unsupported) in place of the old
`seedAllowDecisionForSendPaymentLink`-based rejection test, since
`send_payment_link` is no longer rejected.

**Razorpay verification status is unchanged: NOT VERIFIED.**
`RazorpayProvider.SendPaymentLink` reuses the same unverified HTTP call
as `RetryPayment` — see the verification-status paragraph above, which
applies identically here.

## What is explicitly out of scope (by design)

Webhooks, webhook signature verification, reconciliation, payment outcome
finalization (`SUCCESS`/`FAILED` as a trusted, durable state), automatic
retry loops after an ambiguous result, analytics/reporting, customer
notification infrastructure, a human approval workflow, a policy admin
UI, and any transition beyond `VERIFYING`. `ExecutionEngine` has no code
path toward any of these — establishing financial truth from `VERIFYING`
is Milestone 7's job entirely.
