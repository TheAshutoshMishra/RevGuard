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
                     RecoveryDiagnosis, RecoveryEconomicEvaluation, PolicyDecision,
                     AuditEvent, Money/Currency, ProbabilityBasisPoints value types
  internal/http/     HTTP layer (chi router, handlers)
  internal/infrastructure/  thin wrappers around Postgres/Redis/Redpanda
  internal/repository/      Postgres persistence layer (Create/GetByID per entity,
                            DBTX interface so repos run against pool or tx)
  internal/service/         event validation, idempotent ingestion, RecoveryCase
                            state machine, RecoveryOrchestrator, EventPublisher,
                            RecoveryContextBuilder, AIClient, AnalysisOrchestrator,
                            EconomicEngine, RecoveryProbabilityEstimator, ActionEconomics,
                            PolicyEngine, PolicyConfig, evaluatePolicyRules
  migrations/         SQL migrations (golang-migrate up/down pairs, one per table)
ai-service/          Python FastAPI AI/ML/LLM service
  app/main.py         FastAPI app + /health + POST /v1/diagnose
  app/models/         Pydantic request/response models, controlled vocabularies
  app/providers/      LLMProvider abstraction: MockProvider, AnthropicProvider
  app/services/        DiagnosisService (orchestrates provider call + versioning)
  app/prompts/         versioned system prompts (diagnosis_v1.py)
frontend/            Next.js + TypeScript frontend
deployments/         deployment configuration (future use)
docs/architecture/   architecture notes/diagrams (event-flow.md: Milestone 2 pipeline;
                     ai-diagnosis.md: Milestone 3 AI diagnosis pipeline;
                     economic-engine.md: Milestone 4 economic evaluation pipeline;
                     policy-engine.md: Milestone 5 policy decision pipeline)
docs/decisions/      ADRs (0001: recovery probability vs. AI confidence, and why the
                     Economic Engine doesn't decide; 0002: why AI recommendation,
                     economic evaluation, and policy authorization are three
                     separate layers)
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

### Milestone 3 — AI Diagnosis & Recommendation: COMPLETE

Goal: connect the RecoveryCase lifecycle to the Python AI service for
structured diagnosis, resuming from `ANALYZING` and stopping at
`ANALYZED`. AI recommends; it never authorizes, executes, or mutates
durable state directly. Full design rationale and pipeline diagram:
[`docs/architecture/ai-diagnosis.md`](./docs/architecture/ai-diagnosis.md).

- [x] **AI response contract** (`ai-service/app/models/diagnosis.py`):
      Pydantic `DiagnosisRequest{case_id, context}` /
      `DiagnosisResponse{diagnosis, recommendation, risk_flags,
      explanation, provider, model, prompt_version, generated_at}`.
      `RecoveryContext` carries only identifiers, amounts, statuses, and
      attempt/action history — never card numbers, CVV, credentials, or
      API keys (`domain.Payment` doesn't model those fields, so there's
      nothing to leak). No arbitrary/unvalidated JSON crosses the
      Python -> Go boundary.
- [x] **Controlled vocabularies**, mirrored exactly on both sides
      (`ai-service/app/models/diagnosis.py` and
      `backend/internal/domain/recovery_diagnosis.go`): `FailureCategory`
      (7 values: transient_failure, insufficient_funds,
      payment_method_issue, authentication_issue, mandate_issue,
      customer_abandonment, unknown) and `RecommendedAction` (6 values:
      retry_payment, send_payment_link, request_payment_method_change,
      send_reminder, escalate_to_human, stop_recovery — deliberately a
      distinct Go type from `RecoveryActionType`, so a recommendation can
      never be structurally confused with an authorized action).
- [x] **Confidence validated twice, independently**: `pydantic.Field(ge=0,
      le=1)` in Python, and `validateRecommendation` in
      `backend/internal/service/ai_client.go` on the way into Go (also
      checks action/category are known values and required fields are
      present). Confidence expresses the AI's confidence in its own
      recommendation only — it is never treated as an authorization
      signal.
- [x] **LLM provider abstraction** (`ai-service/app/providers/base.py`):
      `LLMProvider.generate_diagnosis(context) -> ProviderOutput`.
      `MockProvider` (deterministic, rule-based, `name="mock"`,
      `model="mock-rule-based-v1"` — the permanent, explicit signal that a
      diagnosis is not real AI output) is the default
      (`AI_PROVIDER=mock`) and what every test uses. `AnthropicProvider`
      (real calls via the Anthropic Messages API over `httpx`, no vendor
      SDK) requires `ANTHROPIC_API_KEY`; `AI_PROVIDER=anthropic` without
      it fails fast at startup rather than silently falling back.
- [x] **Versioned system prompt** (`ai-service/app/prompts/diagnosis_v1.py`,
      `PROMPT_VERSION = "v1"`) explicitly tells the model it is not
      authorized to execute/approve/reject payments, change policy, call
      infrastructure, or modify durable state — it only ever returns the
      structured recommendation shape.
- [x] **Go AI client** (`backend/internal/service/ai_client.go`):
      `AIClient.Diagnose(ctx, AIRequest) (*AIRecommendation, error)`.
      `HTTPAIClient` is the only place in the codebase that knows HTTP
      transport details for this call. Configurable base URL
      (`AI_SERVICE_URL`) and timeout (`AI_REQUEST_TIMEOUT_SECONDS`,
      default 20s). Retries **at most once**, and only for
      transport-level failures before any response is received
      (connection refused, DNS) — never for non-2xx, an already-exhausted
      timeout, or a malformed/invalid response. Never leaks raw upstream
      response bodies into error messages.
- [x] **Recovery context builder**
      (`backend/internal/service/recovery_context_builder.go`): assembles
      `AIRequest` from `RecoveryCase` + `Payment` + `PaymentAttempt`s (new
      `PaymentAttemptRepository.ListByPaymentID`) + prior
      `RecoveryAction`s (new
      `RecoveryActionRepository.ListByRecoveryCaseID`). All DB access
      lives here; the AI client has none.
- [x] **Database changes**: migration `000012` adds `recovery_diagnoses`
      (new table, not a modification of any Milestone 1/2 table) —
      separate from `recovery_actions`/`recovery_outcomes` by design: a
      diagnosis is a recommendation, an action is something RevGuard
      *decided* to attempt. `CHECK` constraints mirror both controlled
      vocabularies. Rows are immutable; a case can accumulate more than
      one over time. No raw provider response is stored — only the
      validated structured fields plus `provider`/`model`/
      `prompt_version`/`generated_at` for reproducibility.
- [x] **Orchestration** (`backend/internal/service/analysis_orchestrator.go`,
      `AnalysisOrchestrator.AnalyzeCase`): two-phase — the AI call happens
      *outside* any database transaction (an external HTTP call has no
      business holding Postgres connections/locks open), then persisting
      the diagnosis + the guarded `ANALYZING -> ANALYZED` transition
      (`ValidateTransition`, then `RecoveryCaseRepository.UpdateStatus`,
      same guarded-update pattern as Milestone 2) + an `AuditEvent`
      (`ActorType: AI`, distinguishing this from Milestone 2's
      `SYSTEM`-driven `DETECTED -> ANALYZING`) all happen in one
      transaction. `EventProcessor.Process` calls `AnalyzeCase`
      automatically, once, immediately after a fresh case is created —
      after that case-creation transaction has already committed.
- [x] **Failure handling**: an AI failure (timeout, connection refused,
      HTTP 5xx, malformed JSON, invalid confidence, unknown
      action/category) is an analysis failure, never mistaken for a
      payment or recovery outcome. The case is left in `ANALYZING`,
      exactly where it started. `EventProcessor.Process` does not fail the
      whole `POST /events` request when this happens — the event was
      durably ingested and the case was durably created regardless; the
      response carries `case_status: "ANALYZING"` and a separate
      `AnalysisError` string rather than a generic failure.
- [x] **Idempotency**: `AnalyzeCase` is a no-op (no AI call, no persisted
      diagnosis, no transition) whenever the case is not currently
      `ANALYZING` — this is the guard against duplicate analysis. A
      genuine re-analysis (case back in `ANALYZING`) produces a new,
      separate `RecoveryDiagnosis` row rather than overwriting the
      previous one. Nothing in Milestone 3 executes anything, so
      "idempotent" here specifically means "never a duplicate diagnosis
      row for the same analysis attempt, never a duplicate/invalid
      transition" — there is no financial action to duplicate yet.
- [x] Explicitly NOT implemented (by design, per milestone scope): policy
      decisions, economic calculations, payment execution,
      webhook/reconciliation, continuing the state machine past
      `ANALYZED`.

**Tests:**
- Python (36 tests, `ai-service/tests/`, pytest + pytest-asyncio):
  request/response validation (valid request, missing case_id, malformed
  context, invalid fields, confidence out of bounds both directions,
  unknown action, unknown failure category, malformed response);
  `MockProvider` rule coverage; `AnthropicProvider` via
  `httpx.MockTransport` (successful call, timeout, transport error, HTTP
  500, malformed JSON text, schema-invalid JSON, unexpected shape, empty
  API key rejected — no real network calls in tests); `DiagnosisService`
  wrapping/error-propagation; FastAPI route tests (`TestClient`) for
  success/422/502.
- Go (service package): `ai_client_test.go` against `httptest.Server` —
  valid response, malformed JSON, HTTP 500, timeout, connection refused
  (confirms exactly one bounded retry), confidence out of range, unknown
  action, unknown failure category, missing case_id.
  `recovery_context_builder_test.go` (integration, `TEST_DATABASE_URL`) —
  complete context assembly including attempts/actions, and a static
  regression test asserting the marshaled context never contains
  forbidden substrings (`card_number`, `cvv`, `api_key`, `secret`, etc.).
  `analysis_orchestrator_test.go` (integration) — full
  `DETECTED -> ANALYZING -> ANALYZED` lifecycle via a fake in-process
  `AIClient`; AI failure leaves the case in `ANALYZING` with zero
  persisted diagnoses; case-not-found is a clear error; a case not in
  `ANALYZING` is a true no-op (AI client never even called).

**Verification performed:**
- `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...` — clean,
  with and without `TEST_DATABASE_URL`.
- `pytest` in `ai-service/` — all 36 tests pass.
- Full cross-service manual smoke test with **both services running as
  real processes** (not mocked, not Docker — see the Docker limitation
  below): started the real `ai-service` (`AI_PROVIDER=mock`, port 8125)
  and the real Go backend (`AI_SERVICE_URL` pointed at it, port 8182)
  against the native Postgres instance. Seeded a merchant/customer/payment
  via `psql`, `POST /events` with a `payment.failed` event returned `201`
  with `case_status: "ANALYZED"` on the *first* call (the whole
  `DETECTED -> ANALYZING -> AI diagnosis -> ANALYZED` pipeline completing
  within one HTTP request). `psql` confirmed: `recovery_cases.status =
  ANALYZED`; one `recovery_diagnoses` row with `provider=mock,
  model=mock-rule-based-v1, prompt_version=v1`; three `audit_events`
  (`recovery_case.created`/SYSTEM, `recovery_case.transitioned`/SYSTEM for
  Milestone 2's `DETECTED->ANALYZING`, `recovery_case.transitioned`/AI for
  Milestone 3's `ANALYZING->ANALYZED`). Then stopped the `ai-service`
  process and posted a second event for a fresh payment: the backend log
  showed the bounded retry firing exactly once
  (`connect: connection refused`), `POST /events` still returned `201`
  (event ingestion succeeded), `case_status` was `"ANALYZING"` (not
  corrupted, not fabricated), and `psql` confirmed zero
  `recovery_diagnoses` rows for that case.
- Docker's Postgres/ai-service containers remain non-functional in this
  sandbox — same limitation as Milestones 0–2 (`exec format error` even on
  freshly pulled images; a sandbox-level container execution restriction,
  not a project defect). Native Postgres plus real (non-containerized)
  service processes were used for all verification above.

**Real LLM verification:** NO. No LLM API key (e.g. `ANTHROPIC_API_KEY`)
is configured in this sandbox, so `AnthropicProvider` was exercised only
against a fake HTTP transport in tests (`httpx.MockTransport`), never
against the real Anthropic API. The provider abstraction and
`AnthropicProvider` implementation are complete and ready to use the
moment a real key is supplied via `.env` — nothing else needs to change.
Every diagnosis produced during verification (tests and the manual
end-to-end run) came from `MockProvider` and is clearly labeled as such
(`provider: "mock"`, `model: "mock-rule-based-v1"` on every stored row).

### Milestone 4 — Economic Engine: COMPLETE

Goal: deterministically evaluate whether a `RecoveryDiagnosis`'s
recommendation has positive expected economic value. The case remains
`ANALYZED` before and after evaluation — no policy decision, no
execution, no state transition toward `POLICY_CHECK`. Full design
rationale and formulas:
[`docs/architecture/economic-engine.md`](./docs/architecture/economic-engine.md);
architecture decision record:
[`docs/decisions/0001-economic-engine-probability-vs-confidence.md`](./docs/decisions/0001-economic-engine-probability-vs-confidence.md).

- [x] **Economic domain model**
      (`backend/internal/domain/probability.go`,
      `recovery_economic_evaluation.go`): `ProbabilityBasisPoints` (int32,
      0–10000, validated) and `RecoveryEconomicEvaluation` (revenue at
      risk, recovery probability, expected gross recovery, action cost,
      risk cost — all `domain.Money`, i.e. non-negative — plus a signed
      `ExpectedIncrementalValueMinorUnits int64`, deliberately not `Money`
      since it can be negative).
- [x] **AI confidence is NOT recovery probability** — a deliberate,
      documented architectural boundary (see the ADR). `EconomicEngine`
      never reads `RecoveryDiagnosis.Confidence`.
- [x] **`RecoveryProbabilityEstimator`** interface
      (`backend/internal/service/recovery_probability_estimator.go`) +
      `HeuristicProbabilityEstimator` (`estimator_name="heuristic"`,
      `estimator_version="heuristic-v1"`) — deterministic, rule-based,
      explicitly NOT machine learning, makes NO calls to the AI service
      or anywhere else. Formula: per-failure-category base rate ×
      per-action multiplier, minus penalties for repeated payment
      attempts and prior recovery actions, clamped to [0, 10000]. Every
      coefficient is documented in-line as an illustrative assumption,
      not a measured benchmark.
- [x] **`ActionEconomics`** (`backend/internal/service/action_economics.go`):
      one entry per `domain.RecommendedAction` (all six — no new values
      added), each with a fixed `ActionCostMinorUnits` and a
      `RiskCostBps`. `EconomicModelVersion = "economic-model-v1"`.
      Illustrative RevGuard-v1 demonstration defaults, explicitly not
      real Razorpay costs. Unknown actions rejected
      (`ErrUnknownRecommendedAction`), never silently defaulted.
- [x] **Formulas** (`backend/internal/service/economic_calculations.go`,
      pure functions, no I/O): `expected_gross_recovery = revenue_at_risk
      * probability_bps / 10000`; `risk_cost = revenue_at_risk *
      risk_cost_bps / 10000`; `expected_incremental_value =
      expected_gross_recovery - action_cost - risk_cost`. Rounding:
      standard Go integer division on non-negative operands (floor for
      non-negative values) — the only rounding rule anywhere in the
      engine.
- [x] **Database**: migration `000013` adds `recovery_economic_evaluations`
      (new table; no Milestone 1–3 migration modified) — FKs to
      `recovery_cases`/`recovery_diagnoses`, `BIGINT` monetary columns,
      `INTEGER` probability with a `CHECK (0 <= x <= 10000)`, a `CHECK`
      mirroring the six `RecommendedAction` values, and
      `UNIQUE(recovery_diagnosis_id)` — the idempotency guarantee.
- [x] **`EconomicEngine`** (`backend/internal/service/economic_engine.go`,
      `Evaluate(ctx, recoveryCaseID, recoveryDiagnosisID)`): loads the
      case and diagnosis, validates the diagnosis belongs to the case
      (`ErrDiagnosisCaseMismatch`) and is structurally valid, checks
      idempotency, loads payment attempts + prior recovery actions,
      calls the estimator, looks up action economics, computes all four
      figures, persists the evaluation, writes an
      `AuditEvent` (`recovery_economics.evaluated`, `ActorType: SYSTEM`).
      Makes no external network call, so — unlike `AnalyzeCase`'s
      two-phase structure for the AI call — does all of its work,
      reads included, inside **one** short transaction.
- [x] **Idempotency**: `RecoveryEconomicEvaluationRepository.TryCreate`
      uses `INSERT ... ON CONFLICT (recovery_diagnosis_id) DO NOTHING` —
      unlike a plain `INSERT` hitting a real unique-violation error, this
      never errors and never poisons the transaction, so no `SAVEPOINT`
      is needed (contrast with Milestone 2's case-creation race, which
      does need one). A new diagnosis (re-analysis) gets its own,
      independent evaluation row.
- [x] **Orchestration integration**: `EventProcessor.Process` calls the
      new `EconomicEvaluator` interface (satisfied by `*EconomicEngine`)
      immediately after a successful AI analysis
      (`result.Analyzed && result.Diagnosis != nil`) — a distinct step
      from `AnalysisOrchestrator`, not a modification to it.
      `ProcessResult` gained `EconomicEvaluation`/`EconomicEvaluationError`
      fields, mirroring the `Diagnosis`/`AnalysisError` pattern from
      Milestone 3. Evaluation failure does not fail the `POST /events`
      request, same rationale as AI-analysis failure.
- [x] **Read endpoint**: `GET /v1/recovery-cases/{id}/economic-evaluation`
      (`backend/internal/http/economic_evaluation.go`) — minimal,
      read-only, returns the latest evaluation for a case. No endpoint
      exists (or was added) to approve, execute, or otherwise act on an
      evaluation.
- [x] Explicitly NOT implemented (by design, per milestone scope): policy
      engine, `ALLOW`/`BLOCK`/`ESCALATE` decisions, policy thresholds,
      payment execution, Razorpay API calls, webhooks, reconciliation,
      any transition out of `ANALYZED`. `EconomicEngine` has no code path
      toward any of these.

**Tests (58 total across the Go suite pass, 0 failing, with
`TEST_DATABASE_URL` set — see "Verification performed" below for the
no-DB count):**
- `backend/internal/domain/probability_test.go`: 0, 1, 5000, 9999, 10000
  accepted; -1, -5000, 10001, 20000 rejected.
- `backend/internal/domain/money_test.go`: normal amount, zero, a large
  value (beyond int32 range, confirming `int64`), currency preservation,
  negative rejected, invalid currency codes rejected.
- `backend/internal/service/economic_calculations_test.go` (internal
  `package service` test file, so it can call the unexported
  `calculate*` functions directly): expected-gross-recovery and
  risk-cost formulas including an explicit floor-rounding case
  (`100 * 3333 / 10000 = 33.33 -> 33`), and incremental value for
  positive/zero/negative outcomes (gross recovery >, ==, and < costs).
- `recovery_probability_estimator_test.go`: every one of the 7 failure
  categories and all 6 recommended actions produce an in-range result;
  `stop_recovery` is always exactly 0 bps regardless of category;
  identical inputs called twice produce identical output
  (determinism); more attempts/prior actions strictly lowers probability;
  unknown failure category/action rejected.
- `action_economics_test.go`: all 6 `RecommendedAction` values have
  non-negative cost/risk economics; unknown action rejected;
  `stop_recovery` has zero cost/risk.
- `economic_engine_test.go` (integration, `TEST_DATABASE_URL`): full
  evaluation flow with field-by-field verification (case/diagnosis IDs,
  recommended action, revenue at risk, probability in range, gross
  recovery matches the formula recomputed independently in the test,
  incremental value matches, estimator/model version strings, case
  status unchanged, one audit row); idempotency (evaluate the same
  diagnosis twice -> one evaluation row, one audit row, same evaluation
  ID both times); case not found; diagnosis not found; diagnosis
  belonging to a different case than requested
  (`ErrDiagnosisCaseMismatch`); two different diagnoses for the same
  case get two independent evaluation rows; `GetLatestEvaluation`
  returns `ErrNotFound` before any evaluation exists and the correct row
  after.

**Verification performed:**
- `gofmt -l .`, `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass; DB-gated tests skip cleanly (established pattern since
  Milestone 1).
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v`: **58 tests pass, 0 fail**, across
  `internal/domain`, `internal/http`, `internal/repository`,
  `internal/service` — this count includes every Milestone 0–4 Go test;
  nothing regressed.
- `ai-service/`: confirmed zero git changes (`git status --short
  ai-service/` empty) and all 36 Python tests still pass — the AI service
  was not touched, per this milestone's scope.
- Migration `000013` applied cleanly on top of the existing Milestone
  1–3 schema (now at version 13) against the native Postgres instance
  (`localhost:5432`, `revguard`/`revguard` — same fallback used since
  Milestone 0, Docker still non-functional in this sandbox for the same
  reason documented there).
- **Full cross-service manual smoke test** with real (non-Docker,
  non-mocked at the process level) `ai-service` (`AI_PROVIDER=mock`, port
  8126) and Go backend (port 8183) against native Postgres: seeded a
  merchant/customer/payment/payment_attempt (`insufficient_funds`) via
  `psql`, `POST /events` with a `payment.failed` event returned `201`
  with `case_status: "ANALYZED"` on the first call. Exact observed
  values (recorded here verbatim, not fabricated):
  `recommended_action=send_payment_link`,
  `revenue_at_risk_minor_units=49950`, `recovery_probability_bps=3850`
  (base 3500 for `insufficient_funds` × 110% multiplier for
  `send_payment_link`), `expected_gross_recovery_minor_units=19230`
  (`49950*3850/10000` floored), `action_cost_minor_units=200`,
  `risk_cost_minor_units=149` (`49950*30/10000` floored),
  `expected_incremental_value_minor_units=18881`
  (`19230-200-149`) — every figure hand-verified against the formulas
  above. `GET /v1/recovery-cases/{id}/economic-evaluation` returned the
  identical figures. `psql` confirmed: `recovery_cases.status =
  ANALYZED` (unchanged); one `recovery_economic_evaluations` row with
  `estimator_name=heuristic`, `estimator_version=heuristic-v1`,
  `economic_model_version=economic-model-v1`; four `audit_events` in
  order (`recovery_case.created`/SYSTEM,
  `recovery_case.transitioned`/SYSTEM for M2's
  `DETECTED->ANALYZING`, `recovery_case.transitioned`/AI for M3's
  `ANALYZING->ANALYZED`, `recovery_economics.evaluated`/SYSTEM — new
  this milestone). Re-ran `EconomicEngine.Evaluate` against the exact
  same case/diagnosis IDs from this live run (via a temporary
  `backend/cmd/idemcheck` command, deleted immediately after use) and
  confirmed `Created=false` with the identical evaluation ID returned,
  and a direct `psql` count confirmed exactly 1 row — idempotency
  verified against real HTTP-created state, not just isolated test
  fixtures.
- Docker's Postgres/ai-service containers remain non-functional in this
  sandbox — unchanged limitation since Milestone 0, not a project defect.

**Known limitations:**
- The probability estimator is an illustrative heuristic with no
  historical calibration — RevGuard doesn't yet execute any actions or
  record outcomes, so there's no data to calibrate against. See the ADR.
- Action cost defaults are applied without currency conversion (INR-only
  in practice).
- `GET /v1/recovery-cases/{id}/economic-evaluation` returns only the
  latest evaluation per case, not full history.
- No real LLM was re-verified this milestone (unchanged from Milestone
  3 — no `ANTHROPIC_API_KEY` configured in this sandbox); this milestone
  did not touch the AI service or its provider abstraction at all.

**Explicitly confirmed NOT implemented this milestone:** policy engine,
`ALLOW`/`BLOCK`/`ESCALATE` decisions, payment execution, Razorpay API
calls, payment retries/links as actual side effects, webhooks,
reconciliation, any Redis-based financial state, dashboard/frontend
work, machine-learning training, real historical probability
calibration. The `RecoveryCase` remains `ANALYZED` after economic
evaluation in every test and in the manual verification above.

### Milestone 5 — Policy & Safety: COMPLETE

Goal: deterministically decide ALLOW/BLOCK/ESCALATE for a diagnosed,
economically-evaluated recommendation, transitioning
`ANALYZED -> POLICY_CHECK -> {ALLOW, BLOCK, ESCALATE}`. The Policy Engine
is the final authority before execution (a future milestone) — it never
executes anything, never calls the AI service, never calls a payment
gateway. Full design rationale and rule table:
[`docs/architecture/policy-engine.md`](./docs/architecture/policy-engine.md);
architecture decision record:
[`docs/decisions/0002-three-layer-separation.md`](./docs/decisions/0002-three-layer-separation.md).

- [x] **Policy domain model**
      (`backend/internal/domain/policy_decision.go`):
      `PolicyDecisionOutcome` (ALLOW/BLOCK/ESCALATE — deliberately the
      same strings as the corresponding `RecoveryCaseStatus` values, since
      the outcome *is* the case's next status, unlike `RecommendedAction`
      vs. `RecoveryActionType` where the vocabularies were kept distinct
      on purpose), `PolicyReasonCode` (8 typed codes:
      `STOP_RECOVERY_RECOMMENDATION`, `LOW_AI_CONFIDENCE`,
      `NEGATIVE_EXPECTED_VALUE`, `AMOUNT_ABOVE_AUTO_LIMIT`,
      `MAX_ATTEMPTS_REACHED`, `TOO_MANY_PRIOR_ACTIONS`,
      `ACTION_NOT_AUTO_ALLOWED`, `POLICY_ALLOWED`), and `PolicyDecision`
      (references the exact `RecoveryDiagnosisID` and
      `RecoveryEconomicEvaluationID` evaluated; `AuthorizedAction` set
      only when `Outcome == ALLOW`; immutable after creation).
- [x] **Policy configuration**
      (`backend/internal/service/policy_config.go`, `DefaultPolicyConfig`,
      version `policy-v1`): `MinimumConfidence` (float64, matching
      Milestone 3's existing `RecoveryDiagnosis.Confidence` type — not
      redesigned), `MaxAutoAmountMinorUnits` (int64 — also serves as the
      "human approval threshold" from the brief; see "one threshold, two
      names" in the architecture doc for why these were deliberately
      unified rather than duplicated), `MinimumExpectedIncrementalValueMinorUnits`
      (int64), `MaxPaymentAttempts`/`MaxPriorRecoveryActions` (int),
      `AutoAllowedActions` (`map[domain.RecommendedAction]bool`). All
      monetary fields are integer minor units; nothing is float except
      `MinimumConfidence`, matching M3's existing contract. Thresholds are
      documented illustrative RevGuard defaults, not claimed production
      Razorpay policy.
- [x] **Deterministic rules** (`backend/internal/service/policy_rules.go`,
      `evaluatePolicyRules` — pure function, no I/O): evaluates every
      rule (does not short-circuit), collects every triggered reason
      code, and picks the final outcome by severity
      (BLOCK > ESCALATE > ALLOW), not evaluation order. `ALLOW` only
      happens when zero rules fire. See the architecture doc's rule
      table for the exact 7 rules (B–H) and the merge of rule I into E.
- [x] **Database**: migration `000014` adds `policy_decisions` (new
      table; no Milestone 1–4 migration modified) — FKs to
      `recovery_cases`/`recovery_diagnoses`/`recovery_economic_evaluations`,
      `CHECK` on `decision` (3 values) and `authorized_action` (6 values
      or NULL), and
      `UNIQUE(recovery_case_id, recovery_diagnosis_id,
      recovery_economic_evaluation_id, policy_version)` — the idempotency
      guarantee, exactly the composite key the milestone brief specified.
- [x] **`PolicyEngine`** (`backend/internal/service/policy_engine.go`,
      `Evaluate(ctx, recoveryCaseID, recoveryDiagnosisID,
      recoveryEconomicEvaluationID)`): idempotency check first (before
      any other validation — see the architecture doc for why this
      ordering matters), then loads/validates case+diagnosis+evaluation
      ownership, requires `RecoveryCase.Status == ANALYZED`, evaluates
      rules, persists the decision, performs both state-machine hops
      (`ANALYZED -> POLICY_CHECK -> <outcome>`) via the existing
      Milestone 2 `ValidateTransition`/guarded `UpdateStatus`, writes an
      audit event, commits — all in **one** transaction (no external call
      exists in this engine at all, unlike `AnalysisOrchestrator`'s AI
      call).
- [x] **State machine**: no change — `ANALYZED -> POLICY_CHECK ->
      {ALLOW, BLOCK, ESCALATE}` was already declared in
      `state_machine.go` since Milestone 2. `ALLOW -> EXECUTING`,
      `BLOCK -> CLOSED`, `ESCALATE -> *` remain unimplemented (`ESCALATE`
      still has no outgoing edge), exactly as designed for later
      milestones.
- [x] **Idempotency & concurrency**:
      `PolicyDecisionRepository.TryCreate` uses
      `INSERT ... ON CONFLICT (...) DO NOTHING` (same pattern as
      Milestone 4's `RecoveryEconomicEvaluationRepository.TryCreate`) —
      never errors on conflict, so no `SAVEPOINT` is needed for the race
      fallback (contrast with Milestone 2's case-creation race).
      PostgreSQL's unique constraint is the sole authority; no Redis lock
      introduced.
- [x] **Orchestration integration**:
      `EventProcessor.Process` calls the new `PolicyEvaluator` interface
      (satisfied by `*PolicyEngine`) immediately after a successful
      economic evaluation. `ProcessResult` gained
      `PolicyDecision`/`PolicyEvaluationError` fields, mirroring the
      `Diagnosis`/`Analysis*` and `EconomicEvaluation`/`Economic*`
      pattern from Milestones 3–4. **Chosen failure behavior** (per the
      milestone's explicit request to document this): a policy evaluation
      failure does not fail `POST /events` (the event/case/diagnosis/
      evaluation are already durable regardless), leaves the case at
      `ANALYZED` (never partially transitioned), and creates **no**
      `PolicyDecision` row — never a fabricated default decision.
- [x] **Read endpoint**: `GET /v1/recovery-cases/{id}/policy-decision`
      (`backend/internal/http/policy_decision.go`) — minimal, read-only,
      latest decision for a case. No approve/override/execute capability
      anywhere.
- [x] Explicitly NOT implemented (by design, per milestone scope):
      execution of any kind, Razorpay API calls, payment retries/links as
      real side effects, customer communication, webhooks,
      reconciliation, Redis-based financial state, policy admin UI,
      machine learning, any transition beyond
      `ALLOW`/`BLOCK`/`ESCALATE`. Every integration test asserts zero
      `recovery_actions` rows after every decision.

**Tests (96 total across the Go suite pass, 0 failing, with
`TEST_DATABASE_URL` set — up from 58 after Milestone 4, so ~38 new tests
this milestone, zero regressions):**
- `backend/internal/domain/policy_decision_test.go`: valid/invalid
  `PolicyDecisionOutcome` and `PolicyReasonCode` values; a dedicated test
  asserting `PolicyDecisionOutcome`'s three string values exactly match
  the corresponding `RecoveryCaseStatus` strings (guards the intentional
  coupling `PolicyEngine.Evaluate` relies on).
- `backend/internal/service/policy_rules_test.go` (internal `package
  service` test file, so it can call `evaluatePolicyRules` directly, no
  database): one test per rule (stop_recovery→BLOCK, low
  confidence→ESCALATE, negative expected value→BLOCK, zero expected
  value→BLOCK when minimum is configured positive, amount above auto
  limit→ESCALATE, max payment attempts→BLOCK, too many prior
  actions→ESCALATE, action not auto-allowed→ESCALATE, safe case→ALLOW);
  a multi-reason test proving BLOCK outranks ESCALATE when both fire and
  both reason codes are recorded; determinism (identical input twice →
  identical output); and 8 explicit boundary tests (confidence exactly
  at threshold vs. one unit below; expected value exactly at threshold
  vs. one unit below; amount exactly at limit vs. one minor unit above;
  attempts exactly at maximum vs. one below) — all exact integer/float
  comparisons, no approximation.
- `backend/internal/service/policy_engine_test.go` (integration,
  `TEST_DATABASE_URL`): successful evaluation reaching each of
  ALLOW/BLOCK/ESCALATE with full field verification (decision fields,
  case status, `AuthorizedAction` set only for ALLOW); diagnosis/case
  mismatch; evaluation/case mismatch; evaluation/diagnosis mismatch
  (evaluation computed for a different diagnosis than requested); missing
  evaluation; case not found; case not in ANALYZED status; idempotency
  (evaluate twice → one row, one audit row, identical decision ID and
  fields both times — immutability); **5-goroutine concurrent evaluation**
  of the identical input tuple → exactly one decision row, exactly one
  `Created=true`, all five converge on the same decision ID, final case
  status consistent; `GetLatestDecision` read path; and every ALLOW/BLOCK/
  ESCALATE test asserts zero `recovery_actions` rows (no execution) plus
  the `recovery_policy.evaluated` audit event.

**Verification performed:**
- `gofmt -l .`, `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass; DB-gated tests skip cleanly.
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v`: **96 tests pass, 0 fail**, across `internal/domain`,
  `internal/http`, `internal/repository`, `internal/service` — every
  Milestone 0–5 Go test; nothing regressed.
- `ai-service/`: confirmed zero git changes and all 36 Python tests still
  pass — the AI service was not touched.
- Migration `000014` applied cleanly on native Postgres (now at version
  14).
- **Full cross-service manual smoke test**, real (non-Docker) `ai-service`
  (`AI_PROVIDER=mock`, port 8127) + Go backend (port 8184) + native
  Postgres: seeded a merchant/customer/payment/payment_attempt
  (`insufficient_funds`) via `psql`, `POST /events` with a
  `payment.failed` event returned `201` with `case_status: "ALLOW"` on
  the *first* call — the entire
  `DETECTED -> ANALYZING -> ANALYZED -> economic evaluation ->
  POLICY_CHECK -> ALLOW` pipeline completing within one HTTP request.
  This is the genuine outcome of the deterministic rules against
  `DefaultPolicyConfig` and the mock provider's real diagnosis — it was
  not tuned to force this result (see the milestone brief's explicit
  instruction not to). `GET /v1/recovery-cases/{id}/policy-decision`
  returned `decision=ALLOW, authorized_action=send_payment_link,
  policy_version=policy-v1, reason_codes=[POLICY_ALLOWED]`, with a full
  `Explanation` string showing every threshold compared
  (`confidence=0.750 (min=0.600)`,
  `revenue_at_risk_minor_units=49950 (max_auto=100000)`,
  `expected_incremental_value_minor_units=18881 (min=0)`,
  `payment_attempts=1 (max=3)`, `prior_actions=0 (max=2)`). `psql`
  confirmed: `recovery_cases.status = ALLOW`; one `policy_decisions` row
  with those exact values; five `audit_events` in order
  (`recovery_case.created`/SYSTEM, `recovery_case.transitioned`/SYSTEM
  for M2, `recovery_case.transitioned`/AI for M3,
  `recovery_economics.evaluated`/SYSTEM for M4,
  `recovery_policy.evaluated`/SYSTEM — new this milestone); **zero**
  `recovery_actions` rows (no execution occurred). Re-ran
  `PolicyEngine.Evaluate` against the exact same case/diagnosis/evaluation
  IDs from this live run (via a temporary `backend/cmd/idemcheck`
  command, deleted immediately after use): `Created=false`, identical
  decision ID returned, `psql` confirmed exactly 1 row — idempotency
  verified against real HTTP-created state, not just isolated fixtures.
- Docker's Postgres/ai-service containers remain non-functional in this
  sandbox — unchanged limitation since Milestone 0, not a project defect.

**Known limitations:**
- Policy thresholds (`DefaultPolicyConfig`) are illustrative RevGuard-v1
  defaults, not derived from historical loss data or real risk modeling.
- The "human approval threshold" and "maximum automatic recovery amount"
  from the milestone brief were deliberately unified into one config
  field (`MaxAutoAmountMinorUnits`) rather than duplicated — see "one
  threshold, two names" in the architecture doc.
- `PriorRecoveryActionCount` is always 0 in current practice, since
  nothing in the codebase creates `RecoveryAction` rows yet (execution is
  Milestone 6) — the rule (G) exists and is fully unit-tested for when
  that changes, but has not been exercised against real non-zero prior
  actions in the integration/e2e tests (only in the pure rule tests,
  which pass an explicit count).
- No real LLM was re-verified this milestone (unchanged from Milestones
  3–4); this milestone did not touch the AI service at all.

**Explicitly confirmed NOT implemented this milestone:** payment
execution, Razorpay API calls, payment retries/links as real side
effects, customer communication, webhooks, reconciliation, Redis-based
financial state, Redpanda consumer infrastructure, frontend/dashboard,
policy admin UI, machine learning, historical probability calibration,
automatic human-approval workflows, manual override functionality. The
`RecoveryCase` never transitions past `ALLOW`/`BLOCK`/`ESCALATE`, and no
`RecoveryAction` is ever created, in every test and in the manual
verification above.

### Milestone 6 — Execution Engine: NOT STARTED

Not yet scoped. Do not begin implementation until explicitly instructed.
Milestone 6 will read an `ALLOW` `PolicyDecision`'s `AuthorizedAction` and
actually perform it (payment retry, payment link send, etc.) via
Razorpay, transitioning `RecoveryCase` through `EXECUTING` ->
`VERIFYING`. Do not implement payment execution, Razorpay integration, or
webhooks until this milestone is explicitly started.

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
- Any controlled vocabulary shared across the Go/Python boundary
  (`FailureCategory`, `RecommendedAction`, and the `RecoveryEventType`
  vocabulary from Milestone 1/2) must be updated on **both** sides
  together — the Go enum's `Valid()`, the Python Pydantic `Enum`, the
  system prompt (for AI-produced vocabularies), and the relevant SQL
  `CHECK` constraint. Never add a value to only one side.
- A slow external call (AI service, and later any payment gateway call)
  never happens inside an open PostgreSQL transaction. Follow the
  two-phase pattern in `AnalysisOrchestrator.AnalyzeCase`: do the
  network call first with no transaction open, then open one short
  transaction to persist the result and perform whatever state
  transition it implies.
- The AI service (`ai-service/`) has no PostgreSQL/Redis/Redpanda
  credentials and must never be given any — it is a stateless HTTP
  service. Go is the only thing with database credentials.
- For a "create unless it already exists" idempotency guard, prefer
  `INSERT ... ON CONFLICT (...) DO NOTHING` over a plain `INSERT` caught
  with `repository.IsUniqueViolation`: `ON CONFLICT DO NOTHING` never
  raises an error, so it never poisons the enclosing transaction and the
  caller can safely re-query for the existing row in the same
  transaction (see `RecoveryEventRepository.TryCreate` and
  `RecoveryEconomicEvaluationRepository.TryCreate`). Reserve the
  plain-`INSERT`-plus-`SAVEPOINT` pattern (see
  `recovery_orchestrator.go`) for cases where the conflicting row's
  existence isn't yet certain at write time and you need the outer
  transaction to survive a real constraint violation.
- Money/probability/cost values are never float/double anywhere —
  `domain.Money` (int64 minor units + currency) and
  `domain.ProbabilityBasisPoints` (int32, 0–10000) are the only
  representations. A value that can be legitimately negative (e.g.
  expected incremental value) is a plain signed integer, never
  `domain.Money`, which rejects negative amounts by construction.
- For an idempotent multi-step `Evaluate`-style engine (EconomicEngine,
  PolicyEngine), check for an existing result for the exact input tuple
  **before** validating the current state of anything else. The target
  entity's *current* state (e.g. `RecoveryCase.Status` no longer being
  the "must be in this state to start" value) is often a direct, expected
  consequence of the prior successful call this request is safely
  retrying — validating state before checking idempotency will wrongly
  reject that retry as an error. See `PolicyEngine.Evaluate`'s ordering
  and its doc comment.
- When a decision/evaluation engine can trigger more than one independent
  rule, prefer evaluating every rule and collecting every triggered
  reason (with the final outcome decided by severity, e.g.
  BLOCK > ESCALATE > ALLOW) over short-circuiting on the first match —
  see `evaluatePolicyRules`. Short-circuiting hides secondary reasons
  that matter for auditability ("why" should list everything that
  applied, not just whichever check happened to run first).
