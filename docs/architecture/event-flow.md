# Event Flow — Milestone 2

This document describes how a revenue-risk event moves through RevGuard,
from ingestion to the first RecoveryCase state transition. It covers only
what Milestone 2 implements: deterministic ingestion, idempotency, case
correlation, and state machine mechanics. AI diagnosis, policy decisions,
economic calculations, action execution, and webhook reconciliation are
later milestones and are not described here beyond their integration
points.

## Pipeline

```
Event (HTTP POST /events today; Redpanda consumer later)
    |
    v
Validation (service.EventInput.Validate)
    |
    v
Idempotency (recovery_events.event_id UNIQUE, enforced by PostgreSQL)
    |
    v
Persistence (recovery_events row, within a transaction)
    |
    v
Recovery Case correlation (service.RecoveryOrchestrator)
    |
    v
State Machine (service.ValidateTransition: DETECTED -> ANALYZING)
    |
    v
Audit (audit_events rows: case created, case transitioned)
    |
    v
[commit]
    |
    v
Domain event publish (service.EventPublisher — best-effort, post-commit)
```

Everything above "commit" happens inside a single PostgreSQL transaction
(`backend/internal/service/event_processor.go`). Everything below it is
best-effort and does not affect the durable record.

### 1. Event input & validation

`backend/internal/service/event_input.go` defines `EventInput`, the raw
envelope:

```json
{
  "event_id": "...",
  "event_type": "payment.failed",
  "aggregate_type": "payment",
  "aggregate_id": "...",
  "merchant_id": "...",
  "occurred_at": "2026-01-01T00:00:00Z",
  "payload": {}
}
```

`EventInput.Validate()` checks every field before anything touches the
database: `event_id` non-empty, `event_type` is one of the vocabulary
established in Milestone 1 (`domain.RecoveryEventType`), `aggregate_type`
non-empty, `aggregate_id`/`merchant_id` are valid UUIDs, `occurred_at` is
RFC3339, and `payload` is present and valid JSON. Any failure returns a
wrapped `service.ErrInvalidEvent`, mapped to HTTP 400.

### 2. Idempotency — PostgreSQL is the durable authority

`recovery_events.event_id` has a `UNIQUE` constraint (Milestone 1
migration `000001`+). `PostgresRecoveryEventRepository.TryCreate` inserts
with `ON CONFLICT (event_id) DO NOTHING` and reports whether a row was
actually inserted. If not, the event has already been durably processed:
the existing row is loaded and returned as-is, with `Duplicate: true` and
**no error** — duplicate delivery is a normal, safe outcome, not a
failure.

**Why PostgreSQL and not Redis:** the locked architecture designates Redis
for idempotency/coordination/cache, but explicitly never as a system of
record. A deduplication key that lives only in Redis can be evicted under
memory pressure or lost on a failover, silently reopening the door to
duplicate processing — the exact failure this constraint exists to
prevent. The `UNIQUE` constraint on `recovery_events.event_id` is
enforced by PostgreSQL itself and survives process restarts, connection
pool churn, and cache eviction. Redis remains available for a future
fast-path check (e.g. to short-circuit obviously-duplicate requests before
opening a transaction), but it is never the source of truth for whether an
event has been processed — PostgreSQL is.

### 3. Recovery case correlation

Only a subset of event types are "qualifying" — they open or advance a
RecoveryCase (`service.IsQualifyingEventType`):

- `payment.failed`
- `checkout.abandoned`
- `subscription.failed`
- `mandate.failed`
- `invoice.overdue`

`payment.succeeded` is a positive signal, not a risk one, and is
persisted but never creates a case. The `recovery.*` lifecycle types
(`recovery.created`, `recovery.analyzed`, etc.) are emitted **by** this
system, not ingested to drive it, and also never qualify.

**Milestone 2 scope limitation:** a qualifying event's `aggregate_type`
must be `"payment"`. Milestone 1 only modeled `Payment` as a resolvable
financial aggregate — `subscription`, `invoice`, and `mandate` do not
exist as first-class entities yet. An event with any other
`aggregate_type` is rejected with `ErrUnsupportedAggregate` (HTTP 422)
rather than silently mishandled. This is a deliberate, documented
boundary, not an oversight.

`RecoveryOrchestrator.HandleQualifyingEvent`
(`backend/internal/service/recovery_orchestrator.go`):

1. Resolves the `Payment` by `aggregate_id`. Not found -> `ErrAggregateNotFound`
   (422). Belongs to a different merchant than the event claims ->
   `ErrMerchantMismatch` (422).
2. Looks up an **open** RecoveryCase for that payment
   (`GetOpenByPaymentID`: `WHERE payment_id = $1 AND status <> 'CLOSED'`).
   - **Found:** this event is corroborating evidence for an already-open
     case. It is linked to that case; no new case, no re-transition, no
     duplicate audit record.
   - **Not found:** a new case is created in `DETECTED`, then immediately
     transitioned to `ANALYZING` (see below), each step audited.

#### Why "one open case per payment," not "one case per event"

The spec's "create or locate a RecoveryCase" for a qualifying event means
correlating on the underlying financial aggregate, not on the event
itself: if `payment.failed` and later `mandate.failed` both describe the
same payment while a case is still open, they should attach to the same
case rather than spawning a second one. Migration `000011` enforces this
at the database level with a partial unique index:

```sql
CREATE UNIQUE INDEX idx_recovery_cases_open_payment_unique
    ON recovery_cases (payment_id)
    WHERE status <> 'CLOSED';
```

### 4. Concurrency

Two workers processing the same `payment_id` concurrently (whether the
same `event_id` retried, or two different qualifying events for the same
payment) must not create two open cases. `PostgresRecoveryCaseRepository`
relies on the unique index above rather than application-level locking:

- If both workers' `GetOpenByPaymentID` return "not found" before either
  commits, both attempt `INSERT`. PostgreSQL serializes the two inserts;
  the loser gets a unique-violation error (SQLSTATE `23505`).
- The `INSERT` is wrapped in a `SAVEPOINT` (`tx.Begin` on an existing
  `pgx.Tx`) rather than run directly against the outer transaction.
  PostgreSQL poisons an entire transaction after any error until it is
  rolled back — without the savepoint, the loser's already-persisted
  event insert would be unrecoverable inside the same transaction. Losing
  the race only rolls back the savepoint; the outer transaction (event
  insert included) stays usable.
- The loser re-reads `GetOpenByPaymentID`, now finds the winner's
  committed row, and attaches to it instead — no duplicate case, no
  duplicate transition, no duplicate audit record.

This is deliberately a database-correctness solution, not a distributed
lock: no Redis lock is used or required for this milestone.

### 5. State machine

`backend/internal/service/state_machine.go` declares the full
`RecoveryCase` lifecycle as a transition table
(`DETECTED -> ANALYZING -> ANALYZED -> POLICY_CHECK -> {ALLOW, BLOCK,
ESCALATE} -> ...`), even though Milestone 2 only ever exercises
`DETECTED -> ANALYZING`. The table is declared once so later milestones
call `ValidateTransition`, they don't need to edit it. `ValidateTransition`
is pure — no I/O, no state — and rejects any edge not explicitly listed
(e.g. `DETECTED -> SUCCESS`, `ANALYZING -> EXECUTING`, `SUCCESS ->
ANALYZING` all fail).

**Milestone 2 stops at `ANALYZING`.** A freshly created case is
transitioned exactly once, from `DETECTED` to `ANALYZING`, and no further.
Milestone 3 resumes from `ANALYZING` by calling the Python AI service for
diagnosis; nothing in this milestone calls it.

### 6. Audit trail

Every case creation and every transition writes an `AuditEvent`
(`ActorType: SYSTEM` for now — Milestone 2 performs no AI or policy
decisions, so there is no `AI`/`POLICY_ENGINE` actor yet):

- `recovery_case.created` — metadata: `{status, triggering_event_id, triggering_event_type}`
- `recovery_case.transitioned` — metadata: `{from, to, reason}`

### 7. Transaction boundaries

`EventProcessor.Process` opens one PostgreSQL transaction per call and
scopes every repository to it (`repository.DBTX`, satisfied by both
`*pgxpool.Pool` and `pgx.Tx`). The event insert, case creation/lookup, the
state transition, and both audit writes commit or roll back together — it
is never possible to observe "event stored, but case creation silently
failed." The one intentional exception is **publishing**: after a
qualifying event creates a new case, `EventPublisher.Publish` is called
*after* the commit, and its failure is logged but does not roll back
already-committed state. Publishing is a best-effort side channel, not
part of the durable guarantee.

### 8. Redpanda boundary

`service.EventPublisher` is the integration point for Redpanda:

```go
type EventPublisher interface {
    Publish(ctx context.Context, event domain.RecoveryEvent) error
}
```

Milestone 2 ships `LoggingEventPublisher`, which structured-logs the event
and does no network I/O. Wiring a real Kafka/Redpanda producer later means
writing one new type that satisfies this interface — no orchestration code
changes.

## What is explicitly out of scope (by design)

- AI diagnosis, confidence scores, recommendations (Milestone 3).
- Policy decisions (confidence thresholds, retry limits, amount limits,
  approval thresholds) — `POLICY_CHECK` exists as a state name only.
- Economic calculations.
- Payment execution (`RETRY_PAYMENT`, `SEND_PAYMENT_LINK`, etc. are typed
  but never invoked).
- Webhook/reconciliation.
- Real Redpanda producer/consumer wiring.
- Aggregate types other than `"payment"`.
