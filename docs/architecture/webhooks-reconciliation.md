# Webhooks, Reconciliation & Financial Truth — Milestone 7

This document describes how RevGuard determines what actually happened to
money, after Milestone 6 leaves a `RecoveryCase` in `VERIFYING`. It covers
signature-verified webhook ingestion, on-demand reconciliation, and the one
function both paths share to make `RecoveryCase.Status` a durable, trusted
`SUCCESS`/`FAILED`/`UNKNOWN`.

**The central rule: "the execution request was accepted" is never
"the money moved."** A definitive `SUCCEEDED` `RecoveryAction` from
Milestone 6 means the provider accepted a retry attempt (for
`RazorpayProvider`, that a Payment Link was created) — not that the
customer paid. Only a signature-verified webhook or an explicit
reconciliation lookup against the provider's own state can produce a
`SUCCESS` `RecoveryOutcome`.

## Purpose

By the end of Milestone 6, a `RecoveryCase` that reached `ALLOW` and was
executed sits in `VERIFYING` with exactly one `RecoveryAction`, whose own
`Status` (`SUCCEEDED`/`FAILED`/`UNKNOWN`) describes only whether the
*execution request itself* was accepted — never whether revenue was
recovered. Milestone 7 is the only place that ever moves a `RecoveryCase`
out of `VERIFYING`, and it does so from exactly two kinds of evidence:

- **Webhooks** (`WebhookProcessor`): passive, provider-initiated,
  signature-verified notifications.
- **Reconciliation** (`ReconciliationEngine`): active, on-demand lookups
  against the provider's own authoritative state, plus a shortcut for
  evidence RevGuard already has (an action that itself definitively
  failed at execution time).

Both share one function — `applyFinancialOutcome`
(`backend/internal/service/financial_outcome.go`) — for the actual
`RecoveryCase.Status` transition and `RecoveryOutcome` persistence, so
webhook and reconciliation evidence are reconciled through identical,
once-only, monotonic logic rather than two subtly different code paths.

## Webhook flow

```
POST /v1/webhooks/razorpay (raw body)
  -> WebhookSignatureVerifier.Verify(rawBody, X-Razorpay-Signature)
       fail -> 401, ErrInvalidWebhookSignature, NO database write at all
  -> ProviderEventParser.Parse(rawBody, X-Razorpay-Event-Id)
       fail -> 400, ErrMalformedWebhookPayload, NO database write at all
  -> WebhookProcessor.ingest (one transaction):
       - correlate ParsedProviderEvent.ProviderReference to a
         RecoveryAction via (provider, provider_reference)
         [never via any case/action id the payload might claim]
       - TryCreate the ProviderWebhookEvent row
         (ON CONFLICT (provider, provider_event_id) DO NOTHING)
           not created -> reload, return Duplicate=true, nothing else
                           written (idempotent, safe redelivery)
       - unmatched (no correlated RecoveryAction) -> commit, return;
         durably recorded but no financial effect
       - matched + PENDING status -> commit, return; case unchanged
       - matched + CAPTURED/FAILED -> compute recovered amount, then
         applyFinancialOutcome (VERIFYING -> SUCCESS/FAILED)
```

Signature verification (`webhook_signature.go`) is Razorpay's documented
scheme: HMAC-SHA256 of the **exact raw request body**, hex-encoded, sent
in `X-Razorpay-Signature`, compared with `hmac.Equal` (constant-time —
naive `==` would leak timing information about how many leading bytes
matched). `NewConfiguredWebhookVerifier` **fails closed**: with no
`RAZORPAY_WEBHOOK_SECRET` configured, every webhook is rejected, never
silently accepted unverified.

`RazorpayWebhookParser` (`razorpay_webhook_parser.go`) is deliberately
scoped to the Payment Link event lifecycle Milestone 6's
`RazorpayProvider` actually produces — not Razorpay's full webhook
catalog:

| Razorpay event               | Normalized status |
|-------------------------------|-------------------|
| `payment_link.paid`           | `CAPTURED`        |
| `payment_link.cancelled`      | `FAILED`          |
| `payment_link.expired`        | `FAILED`          |
| anything else                 | `PENDING` (never guessed into a definitive outcome) |

Idempotency key: Razorpay's `X-Razorpay-Event-Id` header when present,
else a SHA-256 hash of the raw body (deterministic for exact redelivery).
This is a **documented assumption**, not a confirmed Razorpay behavior —
see "Razorpay verification status" below.

## Reconciliation flow

`POST /v1/recovery-cases/{id}/reconcile` (empty body, the same convention
Milestone 6's `/execute` established) resolves the case's single
`RecoveryAction` server-side and calls `ReconciliationEngine.Reconcile`:

```
1. Case must be VERIFYING (ErrRecoveryCaseNotVerifying) and must have a
   RecoveryAction (ErrNoRecoveryActionForCase) — genuine caller/structural
   errors, not financial ambiguity.
2. RecoveryAction.Status == FAILED (definitive at execution time)
     -> no external call needed at all; propagate directly to
        VERIFYING -> FAILED.
3. RecoveryAction.ProviderReference == "" (only possible when its own
   execution outcome was UNKNOWN)
     -> nothing could ever be looked up; VERIFYING -> UNKNOWN once,
        terminal for automation, awaiting manual review.
4. Otherwise, PaymentReconciler.Reconcile(provider, reference):
     CAPTURED/FAILED (definitive) -> applyFinancialOutcome, same as a
       webhook.
     PENDING (definitive "not resolved yet") -> case unchanged, safe to
       call again later.
     error, and errors.Is(err, ErrReconciliationReferenceNotFound)
       (provider affirmatively has no record) -> VERIFYING -> UNKNOWN,
       the same dead-end treatment as case 3 — unlikely to ever resolve
       differently.
     any other error (timeout, transport failure) -> genuinely ambiguous;
       case unchanged, never guessed, never auto-retried.
```

`PaymentReconciler` (`payment_reconciler.go`) is read-only by
construction: no implementation may execute a payment, create a payment
link, or otherwise cause a new financial side effect — reconciliation
means "find out what already happened," never "perform the action
again." `FakeReconciler` is deterministic (six scenarios: captured,
failed, pending, not-found, timeout, transport-error) and is the default
(mirroring `PAYMENT_PROVIDER`'s selection). `RazorpayReconciler` fetches
the Payment Link resource (`GET /v1/payment_links/{id}`) — see "Razorpay
verification status" below.

## The financial outcome rule

`computeRecoveredAmount` (`financial_outcome.go`) is the one place that
enforces it: a `SUCCESS` `RecoveryOutcome` requires a genuinely positive,
provider-confirmed amount **and** currency. A `CAPTURED` observation
carrying neither — an incomplete webhook payload, or a malformed
reconciliation response — is never guessed into `SUCCESS`; it is logged
and left alone (`webhook.ignored_insufficient_evidence` /
`reconciliation.ignored_insufficient_evidence`), case unchanged. This
mirrors the database's own defense-in-depth: migration `000017`'s
`recovery_outcomes_success_requires_amount` `CHECK` constraint
(`status <> 'SUCCESS' OR recovered_amount_minor_units > 0`) would reject
a zero-amount `SUCCESS` row anyway — the application-layer guard exists
so that failure is a clear, audited no-op instead of a raw constraint
violation surfacing as a 500.

`FAILED`/`UNKNOWN` outcomes always carry a zero amount, but `domain.Money`
still requires a valid currency, so the `RecoveryCase`'s own
`RevenueAtRisk.Currency` is used as a fallback when the observation itself
didn't carry one.

## State transitions

`VERIFYING -> {SUCCESS, FAILED, UNKNOWN}` were already declared in
`state_machine.go` since Milestone 2; Milestone 7 is the first and only
code that exercises them. No other edge is touched — `BLOCK`, `ESCALATE`,
`ANALYZED`, `DETECTED`, etc. have no path to `SUCCESS`/`FAILED`/`UNKNOWN`
in this codebase, structurally, not just by convention.

`UNKNOWN` means "financial truth could not be established, and is not
expected to become establishable through further automation" — a
terminal state for automation, awaiting manual review, not a synonym for
"still pending." It is reached only via the two dead-end cases above
(no provider reference ever recorded, or the provider affirmatively
reports no record of the reference), never merely because a webhook was
inconclusive (`PENDING` leaves the case in `VERIFYING`, still eligible to
resolve later).

## Idempotency & concurrency

Two independent guarantees compose to make this safe under redelivery,
racing webhooks, and racing reconciliation calls, with no Redis lock
anywhere:

1. **`provider_webhook_events` `UNIQUE(provider, provider_event_id)`**
   (migration `000016`), enforced via `TryCreate`'s
   `ON CONFLICT ... DO NOTHING` — the same pattern as every other
   `TryCreate` in this codebase since Milestone 4. A duplicate delivery
   never even reaches the outcome-application step.
2. **The guarded `RecoveryCase.Status` `UPDATE ... WHERE status = 'VERIFYING'`**
   inside `applyFinancialOutcome`, attempted **first**, before the
   `RecoveryOutcome` row is written. PostgreSQL's row-level locking
   serializes concurrent attempts: the loser's `UPDATE` affects zero
   rows (the winner already changed the status), which
   `applyFinancialOutcome` treats as a safe no-op
   (`recovery_outcome.rejected` audit event) — indistinguishable from "a
   webhook and a reconciliation call raced, or two webhooks raced, and
   one already won." This is what makes terminal financial truth
   monotonic: **at most one call to `applyFinancialOutcome` ever
   succeeds for a given case**, regardless of how many webhooks or
   reconciliation attempts arrive, in any order.
   `recovery_outcomes_recovery_action_id_unique` (migration `000017`) is
   the database-level backstop behind that guarantee, not the primary
   mechanism.

Combined, these mean: redelivering the identical webhook any number of
times produces exactly one `ProviderWebhookEvent` row and, if it was the
first delivery to arrive, exactly one `RecoveryOutcome`; a later,
different webhook or reconciliation call for an already-resolved case is
always a safe, audited no-op; and revenue is never double-counted.

## Razorpay verification status

**NOT VERIFIED against a real Razorpay account or current live
documentation in this session** — no `RAZORPAY_KEY_ID` /
`RAZORPAY_KEY_SECRET` / `RAZORPAY_WEBHOOK_SECRET` are configured in this
sandbox, and there is no confirmed outbound network access to Razorpay's
API. This is the same honesty caveat already applied to
`RazorpayProvider` (Milestone 6):

- `RazorpayWebhookVerifier`'s HMAC-SHA256-of-raw-body scheme is written
  from Razorpay's long-documented, stable webhook signing convention.
- `RazorpayWebhookParser`'s envelope shape (`event`,
  `payload.payment_link.entity`, `payload.payment.entity`, `created_at`)
  and the `X-Razorpay-Event-Id` header assumption are written from
  Razorpay's documented Payment Link webhook shape.
- `RazorpayReconciler`'s Payment Link fetch response shape (`status`,
  `amount_paid`, `currency`, `payments[]`) mirrors `RazorpayProvider`'s
  creation response shape from Milestone 6.

None of the above has been exercised against a live endpoint or a
Razorpay Test Mode account. What **was** verified this milestone:

- Every webhook/reconciliation code path (signature accept/reject,
  parsing, correlation, idempotency, concurrency, all three terminal
  states, both dead-end-to-`UNKNOWN` cases) against `FakeReconciler` and
  hand-constructed Razorpay-shaped JSON payloads, run against the
  natively-installed PostgreSQL — see "Tests" in `CLAUDE.md`.
- A full manual smoke test with real (non-Docker) `ai-service` + Go
  backend processes and native Postgres, driving a real case through
  `DETECTED -> ... -> VERIFYING`, then a correctly-signed webhook to
  `SUCCESS`, an incorrectly-signed webhook rejected with `401` and zero
  state change, duplicate webhook delivery correctly reported and
  ignored, and a second case resolved via the `/reconcile` endpoint
  (once inconclusive with a zero-amount fake reconciler, once to
  `SUCCESS` with an amount configured) — see `CLAUDE.md` for exact
  observed values.

Do not claim Razorpay webhook/reconciliation behavior was live-tested
until it has actually been run against a real Razorpay Test Mode account
and current documentation.

## Known limitations

- `RazorpayWebhookParser` only recognizes the Payment Link event
  lifecycle (`payment_link.paid`/`cancelled`/`expired`) — not Razorpay's
  broader webhook catalog, by design (matches `RazorpayProvider`'s own
  Payment-Link-only scope from Milestone 6).
- The `X-Razorpay-Event-Id`-header-or-body-hash idempotency key is an
  assumption, not confirmed Razorpay behavior (see above).
- `ReconciliationEngine.loadAction` assumes at most one `RecoveryAction`
  per case (true given Milestone 6's idempotency design) and picks the
  most recent defensively if that ever changes.
- No automatic/background reconciliation loop exists — no Redpanda
  consumer, no cron, no retry campaign. Reconciliation is always an
  explicit `POST /reconcile` call, the same deliberate scope boundary
  Milestone 6 drew around `/execute`.
- `UNKNOWN` cases have no automated resolution path in this milestone —
  reaching `UNKNOWN` is the end of what Milestone 7 does for that case;
  a human/ops workflow to resolve it further is out of scope.
