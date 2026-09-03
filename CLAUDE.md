# CLAUDE.md

Guidance for Claude Code (and any future session) working in this repository.

## Locked architecture

Do NOT change these technology choices without an explicit user decision:

- **Go** — core backend and authority
- **Python + FastAPI** — AI/ML/LLM service
- **PostgreSQL** — durable source of truth
- **Redis** — idempotency / coordination / cache
- **Redpanda** — event streaming (Kafka API-compatible)
- **Next.js + TypeScript** — frontend
- **Docker Compose** — local development environment

## Core principle

> AI recommends. Policy decides. Infrastructure executes. Webhooks verify.
> Analytics proves.

Concretely:
- The Python AI service (`ai-service/`) only ever produces recommendations.
  It must never call out to infrastructure directly or make authoritative
  decisions.
- The Go backend (`backend/`) is the sole authority for policy decisions and
  state changes. All writes to durable state go through it.
- PostgreSQL is the only system of record. Redis is never used as a system
  of record — only for idempotency keys, coordination, and caching.
- Redpanda carries events between services; it is not a database.
- Webhooks verify that actions actually executed as decided.
- Analytics/reporting proves outcomes after the fact.

## Repository layout

```
backend/            Go core backend
  cmd/server/        main entrypoint (HTTP API)
  cmd/migrate/        migration runner (golang-migrate over backend/migrations)
  internal/config/   env-based configuration
  internal/domain/   domain models: Merchant, Customer, Payment, PaymentAttempt,
                     RecoveryCase, RecoveryAction, RecoveryOutcome, RecoveryEvent,
                     AuditEvent, Money/Currency value types
  internal/http/     HTTP layer (chi router, handlers)
  internal/infrastructure/  thin wrappers around Postgres/Redis/Redpanda
  internal/repository/      Postgres persistence layer (Create/GetByID per entity,
                            DBTX interface so repos run against pool or tx)
  internal/service/         event validation, idempotent ingestion, RecoveryCase
                            state machine, orchestrator, EventPublisher boundary
  migrations/         SQL migrations (golang-migrate up/down pairs, one per table)
ai-service/          Python FastAPI AI/ML/LLM service
  app/main.py         FastAPI app + /health
frontend/            Next.js + TypeScript frontend
deployments/         deployment configuration (future use)
docs/architecture/   architecture notes/diagrams (event-flow.md: Milestone 2 pipeline)
docs/decisions/      ADRs
scripts/             dev/ops scripts
tests/               cross-service/integration tests
docker-compose.yml   local dev orchestration for all services
.env.example         environment variable template
```

## Milestone tracker

### Milestone 0 — Foundation: COMPLETE

Goal: skeletons for every service, wired together via Docker Compose, no
business logic.

- [x] Repository structure (`backend/`, `ai-service/`, `frontend/`, `docs/`,
      `scripts/`, `tests/`, `deployments/`)
- [x] Go backend skeleton (`backend/cmd/server`, `internal/config`,
      `internal/http`, `internal/infrastructure`) — chi router, pgx pool
      wrapper
- [x] Go `/health` endpoint (`backend/internal/http/router.go`), covered by
      `router_test.go`
- [x] Python FastAPI skeleton (`ai-service/app/main.py`)
- [x] Python `/health` endpoint, verified via direct uvicorn run
- [x] Next.js frontend skeleton (`frontend/app`, TypeScript, Tailwind),
      verified with `npm run build`
- [x] PostgreSQL Docker service (`postgres:16-alpine`, healthcheck, volume)
- [x] Redis Docker service (`redis:7-alpine`, healthcheck)
- [x] Redpanda Docker service (`redpandadata/redpanda:v24.2.18`, healthcheck)
- [x] Dockerfiles for backend (multi-stage, non-root user), ai-service
      (slim Python), frontend (multi-stage Next.js build)
- [x] `docker-compose.yml` wiring all six services with healthchecks and
      `depends_on: condition: service_healthy`
- [x] `.env.example` (and local `.env`, gitignored)
- [x] Root `.gitignore` (env files, node_modules, .venv, .next, Go build
      artifacts)
- [x] Root `README.md`
- [x] `CLAUDE.md` (this file)
- [x] Basic verification (see below)

**Verification performed:**
- `go build ./... && go test ./...` in `backend/` — pass
- FastAPI app imported directly and run via uvicorn; `GET /health` →
  `{"status":"ok"}` confirmed with curl
- `npm run build` in `frontend/` — compiles and prerenders successfully
- `docker compose config` — valid
- Full `docker compose up --build` could **not** be completed in this
  sandbox: the sandbox's cached Docker images (alpine-based: redis, postgres,
  and their alpine base layers) fail at runtime with `exec format error`
  despite matching `linux/amd64` architecture metadata, and outbound network
  access to Docker Hub is currently blocked/timing out, so the images could
  not be re-pulled to rule out corruption. This is an environment limitation,
  not a defect in `docker-compose.yml` or the Dockerfiles — re-run
  `docker compose up -d --build` in an environment with registry access to
  confirm full end-to-end orchestration.
- Git repository initialization/commit is intentionally left to the user —
  do not run `git init`/`git add`/`git commit`/`git push` in this repo unless
  explicitly asked.

### Milestone 1 — Domain Model & Database: COMPLETE

Goal: represent the core RevGuard financial domain in Go and persist it in
PostgreSQL, with no business logic (state machine, policy, execution) yet.

- [x] `Money`/`Currency` value types (`backend/internal/domain/money.go`) —
      amounts are integer minor units (`int64`), never float/double. E.g.
      ₹499.50 is `MinorUnits: 49950, Currency: "INR"`. `Currency` is
      validated as a 3-letter uppercase ISO 4217-shaped code.
- [x] Domain structs in `backend/internal/domain`: `Merchant`, `Customer`,
      `Payment`, `PaymentAttempt`, `RecoveryCase`, `RecoveryAction`,
      `RecoveryOutcome`, `RecoveryEvent`, `AuditEvent`. IDs are
      `github.com/google/uuid.UUID`, generated application-side (Go
      controls identity, consistent with "Go owns authority" — no DB-side
      UUID generation/extension dependency).
- [x] Status/type enums are typed strings with a `Valid()` method and a
      `ValidXxx` slice per type, kept in sync with the DB `CHECK`
      constraints: `PaymentStatus`, `PaymentAttemptStatus`,
      `RecoveryCaseStatus` (full 13-state vocabulary: DETECTED, ANALYZING,
      ANALYZED, POLICY_CHECK, ALLOW, BLOCK, ESCALATE, EXECUTING, VERIFYING,
      SUCCESS, FAILED, UNKNOWN, CLOSED — the state machine itself is not
      implemented), `RecoveryActionType`/`RecoveryActionStatus`,
      `RecoveryOutcomeStatus`, `RecoveryEventType`, `AuditActorType`.
- [x] SQL migrations in `backend/migrations` (golang-migrate format,
      `NNNNNN_description.{up,down}.sql`, one pair per table, applied in FK
      dependency order): merchants, customers, payments, payment_attempts,
      recovery_cases, recovery_actions, recovery_outcomes, recovery_events,
      audit_events. Every table has PKs, FKs, `NOT NULL`, `UNIQUE` where
      appropriate (e.g. `(merchant_id, external_payment_id)`,
      `recovery_actions.idempotency_key`, `recovery_events.event_id`),
      `CHECK` constraints mirroring the domain enums and currency format,
      and indexes on FK/lookup columns. Money columns are `BIGINT` minor
      units + `CHAR(3)` currency — never `FLOAT`/`DOUBLE`.
- [x] `backend/cmd/migrate` — a small Go binary wrapping
      `golang-migrate/migrate/v4` (`database/postgres` + `source/file`
      drivers) to apply/roll back migrations: `go run ./cmd/migrate
      -command up|down|version`.
- [x] Postgres repository layer in `backend/internal/repository`: one
      interface + `Postgres*Repository` implementation per entity
      (`Create`, `GetByID`), using the existing `pgxpool.Pool` wrapper from
      `internal/infrastructure`. `ErrNotFound` sentinel for missing rows.
      No business logic lives here — pure struct/SQL translation.
- [x] Basic verification (see below)

**Verification performed:**
- `go build ./... && go vet ./...` in `backend/` — pass
- Docker Compose's Postgres container could not be started in this sandbox
  (see the Docker limitation noted under Milestone 0 — still present:
  cached and freshly re-pulled `postgres:16-alpine`/`golang` images fail at
  container runtime with `exec format error` regardless of network state).
  As a substitute, the sandbox's natively-installed PostgreSQL 16
  (`postgresql-16` apt package, already present, running on `localhost:5432`)
  was used to verify the schema and repository layer end-to-end:
  - Created a local `revguard`/`revguard` role+database matching
    `.env.example` credentials.
  - `go run ./cmd/migrate -command up` — applied all 9 migrations
    successfully; `\dt`/`\d payments` confirmed the expected tables, PKs,
    FKs, `CHECK`s, `UNIQUE`s, and indexes.
  - `go run ./cmd/migrate -command down` followed by `-command up` — full
    rollback and reapply cycle succeeds cleanly.
  - `backend/internal/repository/repository_test.go` (gated behind
    `TEST_DATABASE_URL`, so `go test ./...` skips it when no test DB is
    configured): round-trips every entity through Create/GetByID across
    the full graph (merchant → customer → payment → payment attempt →
    recovery case → recovery action → recovery outcome, plus a recovery
    event and an audit event), asserts the ₹499.50 → `49950`/`INR` money
    round-trip, asserts a customer referencing a nonexistent merchant is
    rejected by the FK constraint, and asserts `GetByID` on a missing ID
    returns `ErrNotFound`. All pass against the native Postgres instance.
  - Once Docker is functional in a real environment, the same
    `docker compose up -d postgres` + `go run ./cmd/migrate -command up`
    flow is the intended path — nothing in the migrations or repository
    code is sandbox-specific.

### Milestone 2 — Event & Recovery Engine: COMPLETE

Goal: deterministic event ingestion, idempotent processing, RecoveryCase
correlation/creation, and the state machine mechanics (DETECTED ->
ANALYZING, then stop — no AI/policy/execution yet). Full design rationale
and pipeline diagram: [`docs/architecture/event-flow.md`](./docs/architecture/event-flow.md).

- [x] **Event contract** (`backend/internal/service/event_input.go`):
      `EventInput{event_id, event_type, aggregate_type, aggregate_id,
      merchant_id, occurred_at (RFC3339), payload (JSON)}`. `Validate()`
      checks every field — non-empty IDs, `event_type` against the
      Milestone 1 `domain.RecoveryEventType` vocabulary, UUID parsing for
      `aggregate_id`/`merchant_id`, RFC3339 timestamp, present+valid-JSON
      payload — before building a `domain.RecoveryEvent`. Failures return
      wrapped `ErrInvalidEvent` (HTTP 400).
- [x] **Idempotent event processing**: `recovery_events.event_id` has been
      `UNIQUE` since Milestone 1. `PostgresRecoveryEventRepository.TryCreate`
      does `INSERT ... ON CONFLICT (event_id) DO NOTHING` and reports
      whether a row was actually inserted. A duplicate is loaded and
      returned as `ProcessResult{Duplicate: true}` with **no error** —
      redelivery is a normal, safe outcome. PostgreSQL is the durable
      authority for this, not Redis (Redis is cache/coordination only per
      the locked architecture and is not used in this milestone at all).
- [x] **Recovery case creation/correlation**
      (`backend/internal/service/recovery_orchestrator.go`): qualifying
      event types (`payment.failed`, `checkout.abandoned`,
      `subscription.failed`, `mandate.failed`, `invoice.overdue` —
      `payment.succeeded` and the `recovery.*` lifecycle types do not
      qualify) are correlated to the **open** RecoveryCase for their
      underlying payment (`GetOpenByPaymentID`: `WHERE payment_id = $1 AND
      status <> 'CLOSED'`), creating one if none exists. **Scope
      limitation, documented and enforced in code:** only
      `aggregate_type: "payment"` is resolvable in this milestone
      (Milestone 1 didn't model subscription/invoice/mandate as first-class
      entities) — anything else returns `ErrUnsupportedAggregate` (422)
      rather than being silently mishandled.
- [x] **Database changes**: migration `000010` adds a nullable
      `recovery_events.recovery_case_id` FK (links an event to the case it
      was correlated to, so a duplicate lookup can answer "what case did
      this map to" without redoing orchestration). Migration `000011` adds
      a **partial unique index**,
      `idx_recovery_cases_open_payment_unique ON recovery_cases (payment_id)
      WHERE status <> 'CLOSED'` — the database-level guarantee that at most
      one open case exists per payment, which is what makes the
      concurrency story below correct without a distributed lock.
- [x] **Concurrency**: two workers racing to create a case for the same
      payment are resolved by the unique index above: the loser's `INSERT`
      fails with SQLSTATE `23505`, caught via
      `repository.IsUniqueViolation`. That `INSERT` runs inside a
      PostgreSQL `SAVEPOINT` (`tx.Begin` on an existing `pgx.Tx`) rather
      than the outer transaction directly — PostgreSQL poisons an entire
      transaction after any error until rollback, so without the
      savepoint the loser couldn't safely re-query for the winner's case
      inside the same transaction as its own (already-persisted) event
      insert. The loser rolls back just the savepoint, re-reads the now-
      committed winning case, and attaches to it. No Redis lock is used or
      needed.
- [x] **Recovery state machine** (`backend/internal/service/state_machine.go`):
      `ValidateTransition(from, to)` is pure (no I/O) and declares the
      **full** lifecycle from `DETECTED` through `CLOSED` even though this
      milestone only ever exercises `DETECTED -> ANALYZING` — later
      milestones call it, they don't need to extend the table. Invalid
      edges (e.g. `DETECTED -> SUCCESS`, `ANALYZING -> EXECUTING`,
      `SUCCESS -> ANALYZING`) are rejected.
- [x] **Audit trail**: every case creation and transition writes an
      `AuditEvent` (`ActorType: SYSTEM`) — `recovery_case.created` with
      `{status, triggering_event_id, triggering_event_type}`, and
      `recovery_case.transitioned` with `{from, to, reason}`.
- [x] **Transactional consistency** (`backend/internal/service/event_processor.go`):
      one PostgreSQL transaction per `Process` call, via the new
      `repository.DBTX` interface (satisfied by both `*pgxpool.Pool` and
      `pgx.Tx`) so every repository can be constructed scoped to that
      transaction. Event insert, case creation/lookup, the state
      transition, and both audit writes commit or roll back together.
      `EventPublisher.Publish` is the one deliberate exception: it runs
      **after** commit and its failure is only logged, never rolled back —
      publishing is a best-effort side channel, not part of the durable
      guarantee.
- [x] **Redpanda boundary**: `service.EventPublisher` interface +
      `LoggingEventPublisher` (structured-logs via `log/slog`, no network
      I/O). Swapping in a real producer later means writing one new type
      satisfying the interface — no orchestration code changes. No Kafka
      client dependency was introduced.
- [x] **`POST /events`** (`backend/internal/http/events.go`): decodes the
      request, delegates entirely to `service.EventProcessor.Process`, and
      maps errors at the boundary — `ErrInvalidEvent` -> 400;
      `ErrAggregateNotFound`/`ErrUnsupportedAggregate`/`ErrMerchantMismatch`
      -> 422; anything else -> 500 with a generic message (raw
      Postgres/driver errors are never leaked to the client — verified
      manually, see below). 201 for a newly processed event, 200 for a
      duplicate.
- [x] Explicitly NOT implemented (by design, per milestone scope): AI
      diagnosis, policy decisions, economic calculations, payment
      execution, webhook/reconciliation. `POLICY_CHECK`/`ALLOW`/`BLOCK`/
      `ESCALATE`/`EXECUTING`/`VERIFYING`/`SUCCESS`/`FAILED`/`UNKNOWN` exist
      as state machine vocabulary only — nothing drives a case into them
      yet.

**Verification performed:**
- `gofmt -l .`, `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` set — all non-DB-gated tests
  pass (state machine unit tests covering every valid edge plus
  `DETECTED->SUCCESS`/`ANALYZING->EXECUTING`/`SUCCESS->ANALYZING` rejection;
  `EventInput.Validate` unit tests covering the valid case and one failure
  per validation rule); DB-gated tests skip cleanly, as in Milestone 1.
- Docker's Postgres container is still non-functional in this sandbox —
  same limitation as Milestone 0/1, reconfirmed this session: even a
  **freshly re-pulled** `postgres:16-alpine` still fails with `exec format
  error` on this host, and so does `golang:1.25-bookworm` (a glibc image,
  ruling out an alpine/musl-specific cause); trivial static binaries
  (`busybox`, `hello-world`) run fine, which points to a sandbox-level
  container execution restriction rather than image corruption or
  architecture mismatch. This is an environment limitation, not a defect
  in this project's Dockerfiles or `docker-compose.yml`.
- As in Milestone 1, the natively-installed PostgreSQL 16
  (`localhost:5432`, role/db `revguard`/`revguard`) was used for real
  verification:
  - `go run ./cmd/migrate -command up` applied migrations `000010` and
    `000011` cleanly on top of the existing Milestone 1 schema (now at
    version 11).
  - `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
    go test ./...` — all integration tests pass, including:
    idempotency (same `event_id` processed twice yields exactly one
    `recovery_events` row and one `recovery_cases` row); case creation for
    every qualifying event type; `payment.succeeded` creates no case;
    unsupported `aggregate_type` is rejected; both audit rows exist after a
    qualifying event; **two concurrency tests** — identical `event_id`
    raced across 5 goroutines (only one durable event/case), and distinct
    `event_id`s for the *same payment* raced across 5 goroutines (exactly
    one case, all five results converge on the same case ID, exactly one
    reports `CaseCreated`). The second concurrency test is what actually
    exercises the `SAVEPOINT` race-recovery path — an earlier version of
    this code without the savepoint failed this exact test with SQLSTATE
    `25P02` ("current transaction is aborted"), which is why the savepoint
    exists.
  - Manual end-to-end smoke test against the running server (`go run
    ./cmd/server` with `POSTGRES_HOST=localhost`): seeded a real
    merchant/customer/payment via `psql`, `POST /events` with a
    `payment.failed` event returned `201` with `case_created: true` and
    `case_status: "ANALYZING"`; re-posting the identical body returned
    `200` with `duplicate: true` and the same `recovery_case_id`; `psql`
    confirmed exactly one `recovery_events` row, one `recovery_cases` row,
    and both audit rows. Also verified the 422 paths
    (`ErrUnsupportedAggregate`, `ErrAggregateNotFound`) return clear
    messages, and confirmed a genuine persistence failure (FK violation
    from a test mistake using a merchant_id that didn't exist) surfaces as
    a generic `{"error":"failed to process event"}` 500 rather than
    leaking the raw Postgres error.

### Milestone 3 — AI Diagnosis & Recommendation: IN PROGRESS

Goal: connect the RecoveryCase lifecycle to the Python AI service for
structured diagnosis, resuming from `ANALYZING` and stopping at
`ANALYZED`. AI recommends; it never authorizes, executes, or mutates
durable state directly.

## Working conventions

- Keep `internal/infrastructure` limited to thin wrappers around external
  systems (Postgres, Redis, Redpanda clients) — no business logic.
- Keep the AI service free of direct infrastructure calls or policy
  decisions — it only returns recommendations to the backend.
- Do not introduce new technologies outside the locked architecture list
  above without explicit user approval.
- Do not run destructive git commands, and do not initialize/commit/push
  git history unless the user explicitly asks — the user manages git
  manually in this project.
- Never represent money as `float`/`double` anywhere in the stack (Go,
  Python, SQL, or the frontend). Always use integer minor units + an
  explicit ISO 4217 currency code, following `domain.Money`.
- Domain entity IDs are UUIDs generated in Go (`github.com/google/uuid`),
  not database-generated — keeps identity assignment under backend
  authority and avoids a Postgres extension dependency.
- This sandbox's Docker daemon cannot execute non-trivial container images
  (`exec format error` even on freshly pulled, architecture-correct
  images) — a sandbox limitation, not a project defect. When Docker-based
  verification is needed here, fall back to the natively-installed
  PostgreSQL 16 (`localhost:5432`, role/db `revguard`/`revguard` created
  for this purpose) rather than assuming the feature is broken.
- Repositories in `internal/repository` are constructed against a
  `repository.DBTX` (satisfied by both `*pgxpool.Pool` and `pgx.Tx`), not
  a concrete pool type — this is what lets `internal/service` scope a
  whole operation (event insert + case create/lookup + audit) to one
  transaction. When an operation inside a transaction can fail with a
  constraint violation that the caller wants to recover from (see the
  open-recovery-case race in `recovery_orchestrator.go`), wrap just that
  operation in a `SAVEPOINT` (`tx.Begin(ctx)` on an existing `pgx.Tx`)
  rather than the outer transaction directly — PostgreSQL poisons an
  entire transaction after any error until rollback, so without a
  savepoint the rest of the transaction becomes unusable too.
