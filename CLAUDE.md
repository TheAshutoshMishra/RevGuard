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
  cmd/evaluate/       Milestone 8 evaluation CLI (synthetic, deterministic,
                     no DB/network — see internal/service/evaluation_*.go)
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
                            PolicyEngine, PolicyConfig, evaluatePolicyRules,
                            ExecutionEngine, PaymentProvider (FakeProvider, RazorpayProvider),
                            WebhookProcessor, WebhookSignatureVerifier (RazorpayWebhookVerifier),
                            ProviderEventParser (RazorpayWebhookParser), ReconciliationEngine,
                            PaymentReconciler (FakeReconciler, RazorpayReconciler),
                            applyFinancialOutcome (shared by WebhookProcessor/ReconciliationEngine),
                            GenerateSyntheticDataset, EvaluationStrategy (FixedRetryStrategy,
                            StaticRulesStrategy, RevGuardStrategy), RunEvaluation (Milestone 8,
                            evaluation_*.go — SYNTHETIC, offline, no DB/network)
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
                     policy-engine.md: Milestone 5 policy decision pipeline,
                     policy profiles added in Milestone 10;
                     execution-engine.md: Milestone 6 execution pipeline,
                     send_payment_link execution added in Milestone 10;
                     webhooks-reconciliation.md: Milestone 7 webhook/reconciliation/
                     financial-truth pipeline; evaluation-engine.md: Milestone 8
                     SYNTHETIC evaluation/revenue-recovery-proof harness,
                     multi-profile comparison added in Milestone 10)
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

### Milestone 6 — Execution Engine: COMPLETE

Goal: turn an `ALLOW` `PolicyDecision` into a bounded, auditable
execution attempt — create a `RecoveryAction`, execute the authorized
action (only `retry_payment` in this milestone) via a `PaymentProvider`
abstraction, and transition `RecoveryCase` through
`ALLOW -> EXECUTING -> VERIFYING`. The engine never trusts a
caller-supplied action, AI recommendation, or client request parameter —
only the persisted `PolicyDecision.AuthorizedAction`, reloaded fresh from
PostgreSQL. Full design rationale:
[`docs/architecture/execution-engine.md`](./docs/architecture/execution-engine.md).

- [x] **Domain**: `RecoveryAction` (`backend/internal/domain/recovery_action.go`)
      gains `Provider`, `ProviderReference`, `ErrorCode` (plain strings,
      `""` = unset, matching `PolicyDecision.AuthorizedAction`'s existing
      convention) and `ExecutionMetadata []byte` (sanitized JSON, never a
      raw provider response). `RecoveryActionStatus` gains `UNKNOWN` —
      execution was attempted but its outcome could not be definitively
      determined; never fabricated into `SUCCEEDED`/`FAILED`.
- [x] **Migration `000015`** extends the existing `recovery_actions`
      table (no new table, no modification of any prior migration): adds
      `provider`, `provider_reference`, `error_code` (nullable),
      `execution_metadata JSONB NOT NULL DEFAULT '{}'`, extends the
      `status` `CHECK` with `UNKNOWN`, and adds a partial unique index
      `(provider, provider_reference) WHERE provider_reference IS NOT NULL`.
      `idempotency_key`'s pre-existing `UNIQUE` (Milestone 1) is reused
      as-is for execution idempotency.
- [x] **`PaymentProvider`** interface
      (`backend/internal/service/payment_provider.go`): `Name() string`,
      `RetryPayment(ctx, RetryPaymentRequest) (RetryPaymentResult, error)`.
      A definitive outcome (success or failure) is `(result, nil)`; an
      **ambiguous** outcome (timeout, transport failure) is a non-nil
      `error` — mirrors `AIClient.Diagnose`'s error-vs-result split
      (Milestone 3). `RetryPaymentRequest` carries no card data, CVV, or
      credentials (nothing to leak — `domain.Payment` doesn't model
      those fields).
- [x] **`FakeProvider`** (`fake_payment_provider.go`): deterministic,
      always `provider="fake"`, five scenarios (`success`,
      `definitive_failure`, `unsupported`, `timeout`,
      `transport_error`), atomic `InvocationCount()` for concurrency
      assertions. The default provider (`PAYMENT_PROVIDER=fake`) unless
      explicitly overridden.
- [x] **`RazorpayProvider`** (`razorpay_provider.go`): a minimal,
      honestly-scoped Razorpay Test Mode adapter. `retry_payment` is
      mapped to creating a Payment Link (`POST /v1/payment_links`), not a
      literal card re-charge — Razorpay's public API has no
      server-to-server force-retry operation, and RBI regulation requires
      customer re-authentication per charge; a `Succeeded` result here
      means "a retry link was created," not "the payment succeeded."
      Credentials (`RAZORPAY_KEY_ID`/`RAZORPAY_KEY_SECRET`) come from
      environment variables only, sent via HTTP Basic Auth, never
      hardcoded or logged; construction fails fast if either is empty.
      **NOT VERIFIED against a real Razorpay account** — see "Real
      Razorpay verification" below.
- [x] **`ExecutionEngine`** (`backend/internal/service/execution_engine.go`,
      `Execute(ctx, recoveryCaseID, policyDecisionID)`): a 6-step
      validation chain (decision exists -> belongs to the case -> is
      `ALLOW` -> has a valid `AuthorizedAction` -> that action is
      implemented (`retry_payment` only) -> the case is currently
      `ALLOW`) before any side effect. Three phases: **Phase 1** (one
      short transaction) validates, creates the `RecoveryAction`
      (`EXECUTING`), transitions `ALLOW -> EXECUTING`, audits, commits;
      **Phase 2** (no transaction open) calls the provider; **Phase 3**
      (one short transaction) persists the result, transitions
      `EXECUTING -> VERIFYING`, audits, commits. No transaction is ever
      held open across the provider call.
- [x] **Idempotency**: `RecoveryAction.IdempotencyKey =
      "policy-decision:<policyDecisionID>"`, enforced by the existing
      `UNIQUE` constraint via `TryCreate`
      (`INSERT ... ON CONFLICT DO NOTHING`, no `SAVEPOINT` needed, same
      pattern as Milestones 4–5). A found existing action is classified
      as terminal (no-op), recently-`EXECUTING` (still genuinely in
      flight — no-op, never call the provider again), or
      stale-`EXECUTING` (>30s old, presumed abandoned/crashed — resolved
      to `UNKNOWN` **without** calling the provider again, since we
      cannot know whether the abandoned attempt ever reached it).
- [x] **Timeout / ambiguous-outcome semantics**: a non-nil error from
      `PaymentProvider.RetryPayment` (timeout, transport failure) is
      **always** persisted as `RecoveryActionStatus = UNKNOWN`
      (`error_code = PROVIDER_RESPONSE_AMBIGUOUS`), never guessed into
      `SUCCEEDED` or `FAILED`, and never automatically retried. The case
      still transitions to `VERIFYING` either way — it is never left
      stuck in `EXECUTING`, and this engine never advances it to
      `SUCCESS`/`FAILED` under any circumstance. `VERIFYING` is where the
      case waits for Milestone 7.
- [x] **Race safety**: the same READ-COMMITTED re-check pattern
      documented for `PolicyEngine` below is proactively applied in
      `phase1` — an idempotency re-check immediately before rejecting a
      wrong-case-status error, closing the same class of race.
- [x] **Read/write HTTP endpoint**:
      `POST /v1/recovery-cases/{id}/execute`
      (`backend/internal/http/execution.go`) — **empty request body**.
      The handler resolves the case's latest `PolicyDecision`
      server-side (reusing `policyDecisionReader.GetLatestDecision` from
      Milestone 5) and passes its ID into `Execute`; there is no `action`
      field anywhere in the request for a client to set, and no "force
      execute" or override endpoint exists anywhere. Errors map to
      404/422/500 without leaking raw errors; the response never
      includes credentials or a raw provider response body.
- [x] **Config**: `PAYMENT_PROVIDER` (`fake` default, or `razorpay`),
      `RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`, `RAZORPAY_BASE_URL` — all
      read from environment variables in `backend/internal/config`,
      wired in `cmd/server/main.go`'s `buildPaymentProvider`, which fails
      fast on an unknown provider name or missing Razorpay credentials.
- [x] **Bug found and fixed during this milestone (not a regression a
      user reported)**: a genuine TOCTOU race in Milestone 5's
      `PolicyEngine.Evaluate` — `TestPolicyEngine_ConcurrentEvaluationConvergesSafely`
      failed intermittently (~30% of runs) with
      `"recovery case is not in ANALYZED status"`. Root cause: under
      PostgreSQL READ COMMITTED, each `SELECT` in a transaction sees a
      fresh snapshot, so the idempotency check (no existing decision) and
      the case-status check (now `ALLOW`) could straddle a concurrent
      transaction's commit. Fixed by re-checking idempotency once more
      immediately before erroring on wrong-state — verified with 30
      consecutive stress runs (previously ~30% failure rate, now 0/30)
      plus 3 full regression suite runs. The identical defensive pattern
      was proactively built into `ExecutionEngine.phase1` from the start.
- [x] Explicitly NOT implemented (by design, per milestone scope):
      webhooks, webhook signature verification, reconciliation, payment
      outcome finalization (`SUCCESS`/`FAILED` as trusted/durable),
      automatic retry after an ambiguous result, analytics, customer
      notification infrastructure, human approval workflow, policy admin
      UI, any transition beyond `VERIFYING`. Execution is **not** wired
      into `EventProcessor.Process` — it only ever runs via the explicit
      `POST /execute` call, unlike Milestones 3–5's automatic pipeline.

**Tests (all passing, `TEST_DATABASE_URL` set for the integration
subset):**
- `fake_payment_provider_test.go` (unit, no DB): all 5 scenarios return
  the correct definitive/ambiguous shape; `Name()`; atomic
  `InvocationCount()` under 20 concurrent goroutines.
- `execution_engine_test.go` (integration, `TEST_DATABASE_URL`, 15
  tests): ALLOW executes successfully end-to-end (field-by-field
  verification: action status, type, provider, provider reference, case
  status, exactly 1 provider invocation, persisted case status, exactly 1
  `recovery_actions` row, `recovery_execution.started` +
  `.completed` audit rows); definitive failure persists `FAILED` with an
  error code and the case still reaches `VERIFYING`; fake-provider
  timeout and fake-provider transport error both persist `UNKNOWN` and
  never `SUCCESS`/`FAILED`, with a `recovery_execution.unknown` audit
  row; `BLOCK` and `ESCALATE` decisions are rejected
  (`ErrPolicyDecisionNotAllow`) with **zero** provider invocations and
  zero `recovery_actions` rows; a case that never actually reached
  `ALLOW` is rejected (`ErrRecoveryCaseNotAllow`) even when paired with a
  structurally-valid `ALLOW` decision, zero provider invocations; missing
  policy decision (`ErrPolicyDecisionNotFound`); a decision belonging to
  a different case (`ErrPolicyDecisionCaseMismatch`); an `ALLOW` decision
  with no `AuthorizedAction` (`ErrMissingAuthorizedAction`), a defensive
  check since `PolicyEngine` itself should never produce this; a real
  `ALLOW` decision for `send_payment_link` (genuinely produced by
  `PolicyEngine`, not fabricated) is rejected with
  `ErrActionNotExecutable` and zero provider invocations, since only
  `retry_payment` is implemented; a duplicate execution request for the
  same policy decision is fully idempotent (same action ID, exactly 1
  provider invocation across both calls); **5-goroutine concurrent
  execution** of the identical policy decision converges on exactly 1
  provider invocation, exactly 1 `recovery_actions` row, exactly 1
  `Created=true`, and a consistent final `VERIFYING` case status; a
  dedicated no-secrets test scans both `recovery_actions.execution_metadata`
  and `audit_events.metadata` after a real execution for a list of
  forbidden substrings (`card_number`, `cvv`, `api_key`, `secret`,
  `password`, `key_secret`, `authorization`) and finds none.

**Verification performed:**
- `gofmt -l .`, `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass; DB-gated tests skip cleanly.
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v` — **every** test across `internal/domain`,
  `internal/http`, `internal/repository`, `internal/service` passes,
  including all 15 new `ExecutionEngine` tests, all 6 new `FakeProvider`
  tests, and every Milestone 0–5 test with zero regressions.
- `ai-service/`: confirmed zero git changes and all 36 Python tests still
  pass — the AI service was not touched, per this milestone's scope.
- Migration `000015` applied cleanly on native Postgres (Docker's
  Postgres container remains non-functional in this sandbox — unchanged
  limitation since Milestone 0, not a project defect; now at schema
  version 15).
- **Full cross-service manual smoke test**, real (non-Docker, non-mocked
  at the process level) `ai-service` (`AI_PROVIDER=mock`, port 8128) +
  Go backend (`PAYMENT_PROVIDER=fake`, port 8188) + native Postgres:
  seeded a merchant/customer/payment/payment_attempt with a generic
  `NETWORK_ERROR` failure code (chosen deliberately so the real mock AI
  provider's rule-based logic — which recommends `send_payment_link` for
  `insufficient_funds` and `request_payment_method_change` for
  authentication codes — falls through to its `transient_failure` /
  `retry_payment` default; this is genuine mock-provider behavior, not
  tuned to force an outcome) via `psql`, then `POST /events` with a
  `payment.failed` event returned `case_status: "ALLOW"` on the *first*
  call — the entire
  `DETECTED -> ANALYZING -> ANALYZED -> economic evaluation ->
  POLICY_CHECK -> ALLOW` pipeline completing within one HTTP request, as
  established in Milestone 5. `POST /v1/recovery-cases/{id}/execute`
  (empty body) then returned `execution_status: "SUCCEEDED"`,
  `case_status: "VERIFYING"`, `provider: "fake"`, a non-empty
  `provider_reference`, `unknown: false`. Repeating the identical
  `POST /execute` call returned the **same** `recovery_action_id` (real
  idempotency against live HTTP-created state, not just isolated test
  fixtures). `psql` confirmed: exactly 1 `recovery_actions` row
  (`status=SUCCEEDED`, `action_type=RETRY_PAYMENT`, `provider=fake`, a
  non-empty `provider_reference`, correct `idempotency_key`); case
  `status=VERIFYING`; a full 7-event audit trail in order
  (`recovery_case.created`/SYSTEM, `recovery_case.transitioned`/SYSTEM
  for M2, `recovery_case.transitioned`/AI for M3,
  `recovery_economics.evaluated`/SYSTEM for M4,
  `recovery_policy.evaluated`/SYSTEM for M5,
  `recovery_execution.started`/SYSTEM,
  `recovery_execution.completed`/SYSTEM — the last two new this
  milestone).
- **Timeout -> UNKNOWN smoke test**: a second real case was driven
  through the identical pipeline to a genuine `ALLOW` (same
  `NETWORK_ERROR` fixture pattern), then executed via a temporary
  `backend/cmd/execcheck` command (deleted immediately after use, same
  pattern as the `idemcheck` tool used for Milestone 4/5 idempotency
  verification) constructing `ExecutionEngine` with
  `FakeProvider(FakeProviderScenarioTimeout)` against the exact
  `recoveryCaseID`/`policyDecisionID` from that live HTTP-created case.
  Result: `action.Status=UNKNOWN`, `case.Status=VERIFYING`. `psql`
  confirmed the case's `status=VERIFYING` (never `SUCCESS` or `FAILED`),
  the `recovery_actions` row has `status=UNKNOWN`,
  `error_code=PROVIDER_RESPONSE_AMBIGUOUS`, and an empty
  `provider_reference`, and the audit trail's final event is
  `recovery_execution.unknown` (not `.completed`).
- Both backend and ai-service processes were stopped cleanly after
  verification; no server was left running.

**Real Razorpay verification: NOT VERIFIED.** No `RAZORPAY_KEY_ID`/
`RAZORPAY_KEY_SECRET` are configured in this sandbox, and this sandbox
has no confirmed outbound network access to Razorpay's live API or
current documentation. `RazorpayProvider` was written against Razorpay's
long-stable, publicly documented Payment Links request/response shape
but has **not** been exercised against a live endpoint, a Razorpay test
account, or re-verified against current API docs in this session. It has
no dedicated automated test (unlike `FakeProvider`, which is fully
covered) because doing so without real network access would only test a
hand-written mock of Razorpay's behavior, which would be misleading to
present as verification. Do not claim Razorpay execution was tested
until it has actually been run against a real Razorpay Test Mode
account.

**Known limitations:**
- Only `retry_payment` has a real execution implementation. The other
  five `RecommendedAction` values are structurally valid, can be
  genuinely authorized by `PolicyEngine` (confirmed: `send_payment_link`
  reaches `ALLOW` in practice, per Milestone 5's own verification), but
  are rejected by `ExecutionEngine` with `ErrActionNotExecutable` rather
  than executed — by design, not oversight; extending coverage is future
  work, not a defect.
- `RazorpayProvider`'s `retry_payment` maps to Payment Link creation, not
  a literal payment re-charge — an interpretation necessitated by
  Razorpay's actual API surface and RBI re-authentication rules, not an
  approximation invented for convenience. See
  `docs/architecture/execution-engine.md` for the full reasoning.
- Execution is not automatically triggered when a case reaches `ALLOW` —
  it requires an explicit `POST /execute` call. This is a deliberate
  scope boundary (the milestone brief's execution-request boundary
  section), not an oversight.
- `executionStaleAfter` (30 seconds) is an illustrative threshold for
  distinguishing "still in flight" from "abandoned," not derived from any
  measured operational data.

**Explicitly confirmed NOT implemented this milestone:** webhooks,
webhook signature verification, reconciliation, payment outcome
finalization as trusted/durable state, analytics/reporting, frontend/
dashboard work, Redpanda consumer infrastructure, customer notification
infrastructure (WhatsApp/SMS/email), human approval workflow, policy
admin UI, Kubernetes, Temporal, a new database or message broker, a new
major framework, broad Razorpay API surface beyond Payment Links,
automatic retry loops after an ambiguous provider response. The
`RecoveryCase` never transitions past `VERIFYING` in every test and in
the manual verification above.

### Milestone 7 — Webhooks, Reconciliation & Financial Truth: COMPLETE

Goal: determine the ACTUAL financial outcome of a recovery action —
"execution succeeded" is never assumed to mean "revenue recovered."
Consume signature-verified Razorpay webhooks, reconcile `VERIFYING`
`RecoveryCase`s/`RecoveryAction`s against the provider's own
authoritative state on demand, and be the first and only place that
transitions a case to a trusted, durable `SUCCESS`, `FAILED`, or
`UNKNOWN`. Full design rationale, flow diagrams, and the Razorpay
honesty caveat:
[`docs/architecture/webhooks-reconciliation.md`](./docs/architecture/webhooks-reconciliation.md).

- [x] **Domain**: `ProviderWebhookEvent`
      (`backend/internal/domain/provider_webhook_event.go`) — the durable,
      normalized, append-only ingestion ledger for inbound webhooks (never
      the raw request body) and the sole idempotency authority for
      redelivery. `ProviderEventStatus` (`CAPTURED`/`FAILED`/`PENDING`) is
      deliberately distinct from `RecoveryOutcomeStatus` — a webhook
      observation is evidence, not itself a financial outcome.
      `RecoveryOutcome` (`recovery_outcome.go`) gained `Provider`,
      `Source` (`WEBHOOK`/`RECONCILIATION`), `ProviderWebhookEventID`,
      `Metadata` — extending the Milestone 1 struct, not replacing it.
- [x] **Database**: migration `000016` adds `provider_webhook_events`
      (new table) with `UNIQUE(provider, provider_event_id)` — the
      idempotency guarantee for at-least-once webhook delivery. Migration
      `000017` extends `recovery_outcomes` (adds the four fields above),
      a `CHECK` requiring `SUCCESS` rows to carry a positive
      `recovered_amount_minor_units` (never a silent "recovered nothing"
      success), and `UNIQUE(recovery_action_id)` — at most one durable
      outcome per execution attempt. Neither migration modifies a prior
      one.
- [x] **`WebhookSignatureVerifier`** (`backend/internal/service/webhook_signature.go`):
      `RazorpayWebhookVerifier` — HMAC-SHA256 of the exact raw request
      body, hex-encoded, constant-time compared (`hmac.Equal`, avoiding a
      timing side channel). `NewConfiguredWebhookVerifier` **fails
      closed**: no `RAZORPAY_WEBHOOK_SECRET` configured means every
      webhook is rejected, never silently accepted unverified.
- [x] **`ProviderEventParser`** (`provider_event.go` interface,
      `razorpay_webhook_parser.go` implementation): turns a
      signature-verified raw body into a provider-agnostic
      `ParsedProviderEvent`. Scoped to the Payment Link event lifecycle
      Milestone 6's `RazorpayProvider` actually produces
      (`payment_link.paid` → `CAPTURED`, `.cancelled`/`.expired` →
      `FAILED`, anything else → `PENDING`, never guessed into a
      definitive outcome) — not Razorpay's full webhook catalog.
- [x] **`WebhookProcessor`** (`webhook_processor.go`, `Process(ctx,
      rawBody, signatureHeader, eventIDHeader)`): verify → parse →
      correlate to a `RecoveryAction` via `(provider, provider_reference)`
      only (never a client-supplied case/action id) → idempotently
      `TryCreate` the `ProviderWebhookEvent` row → apply the financial
      outcome only for a matched, definitive (`CAPTURED`/`FAILED`)
      observation. A signature or parse failure writes **nothing** to the
      database at all.
- [x] **`PaymentReconciler`** (`payment_reconciler.go` interface) is
      read-only by construction — no implementation may execute a
      payment or create a payment link. `Reconcile` returns
      `(result, nil)` for any definitive answer including `PENDING`
      ("not resolved yet, don't guess" — not an error), and a non-nil
      error for anything ambiguous (timeout, transport failure), mirror-
      ing `PaymentProvider.RetryPayment`'s error-vs-result split from
      Milestone 6. `FakeReconciler` (`fake_reconciler.go`, six
      deterministic scenarios) is the default, matching
      `PAYMENT_PROVIDER`'s selection. `RazorpayReconciler`
      (`razorpay_reconciler.go`) fetches the Payment Link resource
      (`GET /v1/payment_links/{id}`) — **NOT VERIFIED** against a real
      Razorpay account (see below).
- [x] **`ReconciliationEngine`** (`reconciliation_engine.go`,
      `Reconcile(ctx, recoveryCaseID)`): requires the case to be
      `VERIFYING` and to have a `RecoveryAction`; an action that itself
      already definitively `FAILED` at execution time needs no external
      call at all (propagated directly); an action with no
      `ProviderReference` (or a provider reporting no record of the
      reference — `ErrReconciliationReferenceNotFound`) is a dead end for
      automation, resolved to `UNKNOWN` once rather than left in
      `VERIFYING` forever; otherwise the provider is asked and a
      definitive `CAPTURED`/`FAILED` answer applies the same way a
      webhook would.
- [x] **`applyFinancialOutcome`** (`financial_outcome.go`): the single
      function both `WebhookProcessor` and `ReconciliationEngine` call to
      actually transition `VERIFYING -> {SUCCESS, FAILED, UNKNOWN}` and
      persist the `RecoveryOutcome` — sharing it is what guarantees both
      evidence sources reconcile through identical, once-only, monotonic
      logic. The guarded `RecoveryCase.Status` `UPDATE ... WHERE status =
      'VERIFYING'` is attempted **first**, before the outcome row is
      written; PostgreSQL's row-level locking means at most one call ever
      succeeds for a given case, and every loser is a safe, audited
      no-op (`recovery_outcome.rejected`) — never an error, never a
      double-counted outcome.
- [x] **The financial outcome rule, enforced in code**:
      `computeRecoveredAmount` never lets a `CAPTURED` observation with a
      non-positive amount or missing currency become a `SUCCESS` outcome
      — logged and ignored instead
      (`webhook.ignored_insufficient_evidence` /
      `reconciliation.ignored_insufficient_evidence`), mirroring the
      database's own `CHECK` constraint at the application layer so the
      failure is a clear no-op, not a raw constraint-violation 500.
- [x] **HTTP endpoints**: `POST /v1/webhooks/razorpay`
      (`backend/internal/http/webhook.go`) — raw body, `X-Razorpay-
      Signature`/`X-Razorpay-Event-Id` headers, always 2xx unless the
      request itself was unverifiable/malformed or a genuine server
      error occurred (duplicate/unmatched are successful, not error,
      outcomes — Razorpay's webhook delivery retries on non-2xx).
      `POST /v1/recovery-cases/{id}/reconcile`
      (`backend/internal/http/reconciliation.go`) — empty body, the same
      convention Milestone 6's `/execute` established; no request field
      lets a client assert an outcome, amount, or provider reference.
- [x] **Config**: `RAZORPAY_WEBHOOK_SECRET` (fails closed when unset);
      `RECONCILER_FAKE_SCENARIO`/`RECONCILER_FAKE_AMOUNT_MINOR_UNITS`/
      `RECONCILER_FAKE_CURRENCY` configure the default `FakeReconciler`
      (amount defaults to `0`, deliberately inert — never fabricates a
      `SUCCESS` outcome unless explicitly configured with a positive
      amount). `cmd/server/main.go`'s `buildPaymentReconciler` mirrors
      `buildPaymentProvider`'s provider selection.
- [x] Explicitly NOT implemented (by design, per milestone scope):
      Next.js dashboard/analytics UI, ML models, Redpanda consumer
      infrastructure, automatic/background reconciliation (no cron, no
      retry campaign — always an explicit `POST /reconcile`, same
      deliberate boundary as Milestone 6's `/execute`), customer
      notification infrastructure, human approval UI, policy admin UI,
      a new database or message broker, broad Razorpay API surface
      beyond Payment Links, any automated resolution path out of
      `UNKNOWN`.

**Tests (all passing; unit tests need no database, integration tests are
gated behind `TEST_DATABASE_URL`):**
- `webhook_signature_test.go` (unit): valid signature, invalid signature,
  tampered body, missing signature header, empty secret rejected at
  construction, `NewConfiguredWebhookVerifier` fail-closed with no
  secret and normal verification with one.
- `razorpay_webhook_parser_test.go` (unit): `payment_link.paid` →
  `CAPTURED` with correct amount/currency/reference, `.cancelled`/
  `.expired` → `FAILED`, an unrecognized event → `PENDING`, malformed
  JSON, missing `event` field, deterministic body-hash fallback when
  `X-Razorpay-Event-Id` is absent, an unrecognized resource shape → empty
  (unmatchable, not malformed) reference.
- `fake_reconciler_test.go` (unit): all six scenarios return the correct
  result/error shape; atomic `InvocationCount()` under 20 concurrent
  goroutines.
- `webhook_processor_test.go` (integration, 13 tests): invalid signature
  never changes financial state; malformed payload and missing required
  fields rejected; a `CAPTURED` webhook establishes `SUCCESS` with the
  exact provider-confirmed amount/currency/source/external-reference; a
  `.cancelled` webhook establishes `FAILED` with zero recovered amount;
  an unrecognized event type (`PENDING`) leaves the case `VERIFYING`
  with zero outcomes; a webhook for an unmatched/unknown resource is
  durably recorded but has no financial effect; duplicate delivery of
  the identical event produces exactly one outcome and one
  `provider_webhook_events` row; a webhook arriving after the case is
  already `SUCCESS` or already `FAILED` is a safe no-op
  (`recovery_outcome.rejected` audited, no double-count); **5-goroutine
  concurrent delivery of the identical event** converges on exactly one
  applied outcome, one outcome row, one `provider_webhook_events` row,
  and a correct total recovered amount (no double-counting); a
  `CAPTURED` event with no definitive amount is never guessed into
  `SUCCESS`; a dedicated no-secrets test scans persisted webhook
  metadata for forbidden substrings and finds none.
- `reconciliation_engine_test.go` (integration, 12 tests): a `CAPTURED`
  reconciliation result establishes `SUCCESS` with the correct
  amount/currency/provider/source and resolves the `UNKNOWN`
  `RecoveryAction` to `SUCCEEDED`; a `FAILED` result establishes `FAILED`
  ("payment link created" ≠ "revenue recovered," exercised directly); an
  already-known execution-time `FAILED` action needs **zero** reconciler
  invocations and still reaches `FAILED`; `PENDING` and both ambiguous
  scenarios (timeout, transport error) leave the case `VERIFYING` with
  zero outcomes — no fabrication; a provider reporting no record of the
  reference resolves to `UNKNOWN`; an action with no provider reference
  at all resolves to `UNKNOWN` with **zero** reconciler invocations; case
  not found, case not `VERIFYING`, and no `RecoveryAction` for the case
  are all rejected with typed errors; a second `Reconcile` call on an
  already-resolved case is rejected (not a silent double-apply); **5-
  goroutine concurrent reconciliation** of the identical case converges
  on exactly one applied outcome and a consistent final `SUCCESS` status.
- `router_test.go` updated for the two new `NewRouter` parameters.
- Full regression: every Milestone 0–6 test still passes, `go vet` and
  `gofmt` clean.

**Verification performed:**
- `gofmt -l .`, `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass; DB-gated tests skip cleanly.
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v`: **157 tests pass, 0 fail**, across `internal/domain`,
  `internal/http`, `internal/repository`, `internal/service` — every
  Milestone 0–7 Go test; nothing regressed.
- `go test ./... -race` (full suite, including
  `TestWebhookProcessor_ConcurrentDuplicateDeliveryNoDoubleCount` and
  `TestReconciliationEngine_ConcurrentReconciliationConvergesSafely`)
  against the same database — clean, no data races reported.
- `ai-service/`: confirmed zero git changes (`git status --short
  ai-service/` empty) and all 36 Python tests still pass — the AI
  service was not touched, per this milestone's scope (Milestone 7 is
  Go-only: webhooks/reconciliation are backend concerns, no new AI
  contract).
- Migrations `000016` and `000017` applied cleanly on native Postgres
  (`localhost:5432`, `revguard`/`revguard` — same fallback used since
  Milestone 0, Docker still non-functional in this sandbox for the same
  documented reason). Schema now at version 17.
- **Full cross-service manual smoke test**, real (non-Docker,
  non-mocked) `ai-service` (`AI_PROVIDER=mock`) + Go backend
  (`PAYMENT_PROVIDER=fake`, `RAZORPAY_WEBHOOK_SECRET=smoke-test-secret`)
  + native Postgres:
  - Seeded a merchant/customer/payment/payment_attempt via `psql`,
    `POST /events` reached `case_status: "ALLOW"` on the first call (the
    established M2→M5 pipeline, unchanged), `POST /execute` reached
    `VERIFYING` with a `fake` provider reference (the established M6
    pipeline, unchanged).
  - `POST /v1/webhooks/razorpay` with an intentionally wrong signature
    returned `401` with zero state change (confirmed via `psql`: case
    still `VERIFYING`, zero `recovery_outcomes` rows).
  - A correctly HMAC-SHA256-signed `payment_link.paid` webhook (built
    with a standalone Python HMAC script, not the application code, for
    an independent signature check) returned `200` with
    `case_status: "SUCCESS"` on the first delivery. `psql` confirmed:
    one `recovery_outcomes` row (`status=SUCCESS`,
    `recovered_amount_minor_units=49950`, `currency=INR`,
    `provider=razorpay`, `source=WEBHOOK`); `recovery_cases.status =
    SUCCESS`; a 10-event audit trail in order ending
    `webhook.received` → `recovery_outcome.recorded` →
    `recovery_case.transitioned` (all `ActorType=WEBHOOK` for the last
    three).
  - Redelivering the **identical** webhook (same `X-Razorpay-Event-Id`)
    returned `200` with `"duplicate": true, "financial_outcome_applied":
    false` — `psql` confirmed still exactly one `recovery_outcomes` row
    and one `provider_webhook_events` row (no double-count).
  - A second case was driven to `VERIFYING` and reconciled via
    `POST /reconcile`: first against the default zero-amount
    `FakeReconciler` — returned `"applied": false`, case remained
    `VERIFYING` (never fabricated `SUCCESS` from a zero amount); then,
    after restarting the backend with
    `RECONCILER_FAKE_SCENARIO=payment_captured
    RECONCILER_FAKE_AMOUNT_MINOR_UNITS=30000`, the identical endpoint
    returned `"applied": true, "case_status": "SUCCESS"` — `psql`
    confirmed one `recovery_outcomes` row
    (`recovered_amount_minor_units=30000, source=RECONCILIATION,
    provider=fake`) and the full 10-event audit trail including
    `reconciliation.ignored_insufficient_evidence` (from the earlier,
    correctly-inconclusive call) followed by `recovery_outcome.recorded`
    and `recovery_case.transitioned`.
  - Both backend and ai-service processes were stopped cleanly after
    verification; no server was left running. A temporary
    `backend/cmd/smokecheck` diagnostic (used only to trace an unrelated
    pre-existing nullable-column scan issue in hand-seeded `psql` test
    fixtures — not an M7 defect) was deleted immediately after use, the
    same pattern as the `idemcheck`/`execcheck` tools from Milestones
    4–6.

**Real Razorpay verification: NOT VERIFIED.** No `RAZORPAY_KEY_ID`/
`RAZORPAY_KEY_SECRET`/`RAZORPAY_WEBHOOK_SECRET` real credentials and no
confirmed outbound network access to Razorpay's API exist in this
sandbox. `RazorpayWebhookVerifier`, `RazorpayWebhookParser`, and
`RazorpayReconciler` are written from Razorpay's long-documented,
publicly stable webhook/Payment-Link behavior but have **not** been
exercised against a live endpoint, a real webhook delivery, or a
Razorpay Test Mode account. See
[`docs/architecture/webhooks-reconciliation.md`](./docs/architecture/webhooks-reconciliation.md)
for exactly what was and wasn't tested. Do not claim live Razorpay
verification until it has actually been performed.

**Known limitations:**
- `RazorpayWebhookParser` only recognizes the Payment Link event
  lifecycle, matching `RazorpayProvider`'s own Milestone 6 scope — not
  Razorpay's full webhook catalog.
- The `X-Razorpay-Event-Id`-header-or-body-hash idempotency key is a
  documented assumption, not confirmed Razorpay behavior.
- No automatic/background reconciliation exists; reaching `UNKNOWN` has
  no further automated resolution path in this milestone — a human/ops
  workflow to resolve it is out of scope.
- The hand-seeded `psql` test fixtures used for manual smoke testing hit
  two pre-existing nullable-column scan issues in the Milestone 1
  repository layer (`payments.payment_method`,
  `payment_attempts.failure_reason` are nullable in the schema but
  scanned into non-nullable `*string` fields) — worked around by setting
  those columns in the fixtures; not an M7 defect, not fixed as part of
  this milestone (out of scope, pre-existing since Milestone 1).

**Explicitly confirmed NOT implemented this milestone:** Next.js
dashboard/analytics work, ML models, Redpanda consumer infrastructure,
Kubernetes, Temporal, a new database or message broker, a new major
framework, broad Razorpay API surface beyond Payment Links, customer
notification infrastructure (WhatsApp/SMS/email), human approval UI,
policy admin UI, automatic retry campaigns, unrelated refactoring. The
`RecoveryCase` never transitions except via the documented
`VERIFYING -> {SUCCESS, FAILED, UNKNOWN}` edges in every test and in the
manual verification above.

### Milestone 8 — Evaluation & Revenue Recovery Proof: COMPLETE

Goal: a deterministic, offline evaluation harness proving whether
RevGuard recovers more incremental revenue than simpler recovery
strategies, without giving AI financial authority, bypassing
`PolicyEngine`, modifying the state machine, or touching Milestone 7's
financial-truth logic. **Every dataset and every result in this
milestone is SYNTHETIC** — none of it is, or has been validated
against, live Razorpay production data. Full design rationale, formulas,
and limitations:
[`docs/architecture/evaluation-engine.md`](./docs/architecture/evaluation-engine.md).

- [x] **Files created** (all in `backend/`, no M0–M7 file modified):
      `internal/service/evaluation_dataset.go` (deterministic
      `SyntheticOpportunity` generator, seeded via `deriveRand(seed,
      index, salt)`), `evaluation_ground_truth.go` (independent,
      strategy-blind `computeGroundTruth`, deliberately using a
      different base-rate table from Milestone 4's
      `heuristicBaseRateBps` — see the architecture doc's "deliberately
      independent" rationale), `evaluation_diagnosis.go`
      (`deterministicDiagnosis`, a rule-based AI-diagnosis stand-in
      mirroring `ai-service`'s existing `MockProvider` — not a network
      call, not new AI), `evaluation_strategies.go`
      (`EvaluationStrategy` interface, `FixedRetryStrategy`,
      `StaticRulesStrategy`, `RevGuardStrategy`), `evaluation_metrics.go`
      (`StrategyMetrics`, `aggregateStrategyMetrics`),
      `evaluation_engine.go` (`EvaluationResult`, `RunEvaluation`,
      `FormatResultTable`), plus five `evaluation_*_test.go` files;
      `cmd/evaluate/main.go` (CLI); `docs/architecture/evaluation-engine.md`.
- [x] **No second RevGuard implementation.** `RevGuardStrategy` calls
      the real, unmodified `HeuristicProbabilityEstimator.Estimate`
      (Milestone 4), `GetActionEconomics` +
      `calculateExpectedGrossRecovery`/`calculateRiskCost`/
      `calculateExpectedIncrementalValue` (Milestone 4), and
      `evaluatePolicyRules` + `DefaultPolicyConfig` (Milestone 5) —
      every formula and threshold is the exact same code the real HTTP
      pipeline uses. The only new logic is the deterministic AI-diagnosis
      stand-in (necessary for offline reproducibility — a real AI call
      would be non-deterministic) and the two independently-implemented
      baselines.
- [x] **Deterministic synthetic dataset**
      (`GenerateSyntheticDataset(seed, count)`): 500–1000+
      `SyntheticOpportunity` values (amount, currency, failure category,
      payment method, customer history, previous attempts, previous
      recovery actions, hours since failure). Every random draw comes
      from a `*rand.Rand` derived purely from `(seed, index, salt)` —
      same seed always reproduces the identical dataset
      (`TestGenerateSyntheticDataset_SameSeedIsIdentical`), different
      seeds diverge (`TestGenerateSyntheticDataset_DifferentSeedIsDifferent`).
- [x] **Ground-truth model** (`computeGroundTruth`): independent of every
      strategy — computed once per opportunity before any strategy runs,
      stored on `SyntheticDataset`'s unexported `groundTruths` field,
      which no `EvaluationStrategy.Decide(opportunity)` signature can
      reach. Uses its own base-rate table (deliberately different from
      RevGuard's own estimator) plus payment-method/customer-history/
      attempt/time modifiers, clamped to [0, 10000] bps, then a
      deterministic random draw decides `Recoverable`.
- [x] **Baseline 1 (Fixed Retry)** and **Baseline 2 (Static Rules)**
      (`evaluation_strategies.go`): independently implemented, no shared
      estimator/diagnosis/policy code with `RevGuardStrategy` — only the
      real `GetActionEconomics` cost table, since the cost of performing
      an action is a fact about the action, not a strategic choice.
- [x] **Fairness/anti-bias**, enforced structurally and tested: all
      three strategies iterate the same `dataset.Opportunities` slice
      (`TestFairness_AllStrategiesSeeSameDataset`); decisions are
      order-independent (`TestFairness_StrategyDecisionOrderIndependent`);
      baselines are unaffected by RevGuard having run
      (`TestFairness_BaselinesCannotSeeRevGuardDecisions`); ground truth
      is unchanged by any strategy run
      (`TestGroundTruth_IndependentOfStrategy`); no output claims live
      Razorpay validation (`TestFairness_NoProductionRazorpayClaims`).
- [x] **Metrics** (`evaluation_metrics.go`, `evaluation_engine.go`) —
      exact formulas in
      [`docs/architecture/evaluation-engine.md`](./docs/architecture/evaluation-engine.md#metrics-and-exact-formulas):
      Revenue At Risk, Potentially Recoverable Revenue, Revenue
      Recovered, Recovery Rate, Incremental Recovered Revenue, Recovery
      Cost, Risk Cost, Net Incremental Value, Incremental Net Value,
      Actions Taken/Blocked, Human Escalations, Unnecessary Actions,
      Average Attempts, Action Reduction %. All monetary figures are
      `int64` minor units; only display ratios (`RecoveryRate`,
      `AverageAttempts`, `ActionReductionPercent`) are `float64`.
- [x] **Reproducibility**: `RunEvaluation(seed, cases)` is a pure
      function — no timestamps, no random UUIDs, nothing non-deterministic
      anywhere in `EvaluationResult`. Verified by
      `TestRunEvaluation_Reproducible` (full `reflect.DeepEqual` plus a
      byte-for-byte JSON comparison) and manually via two separate CLI
      runs diffed with `diff` (see "Verification performed" below).
- [x] **Machine-readable output**: `EvaluationResult` marshals to the
      JSON shape in the milestone brief (`dataset`/`strategies`/
      `comparisons`, plus a `disclaimer` field) via
      `go run ./cmd/evaluate --seed <seed> --cases <cases> [--output
      evaluation.json]`. **Human-readable output**:
      `service.FormatResultTable` renders the CLI table, always printed
      to stdout by the CLI regardless of `--output`.
- [x] Explicitly NOT implemented (by design, per milestone scope):
      Next.js dashboard/charts, ML training/reinforcement learning,
      Kubernetes, Temporal, a Redpanda consumer, automatic production
      retry campaigns, customer notifications, human approval/policy
      admin UI, live Razorpay production integration, a new database or
      message broker, any M9 work. No M0–M7 file was modified except
      `CLAUDE.md` (this section) — `git status` confirms
      `backend/internal/service/{event_processor,execution_engine,
      webhook_processor,reconciliation_engine,policy_engine,
      economic_engine}.go` and every other pre-existing file are
      untouched.

**Test counts:** 46 new tests across the 5 `evaluation_*_test.go` files
(dataset determinism/bounds/uniqueness, ground-truth determinism/
clamping/independence, per-strategy decision tests for all three
strategies including determinism, metrics-formula tests — recovery
rate, net incremental value, unnecessary actions, all-recoverable,
all-unrecoverable, zero-opportunity, large monetary values — full
`RunEvaluation` reproducibility/negative-input/comparison-consistency/
disclaimer tests, and the 5 fairness/anti-bias tests above). Combined
with the pre-existing Milestone 0–7 suite, the full `go test ./...`
(with `TEST_DATABASE_URL` set) run is **203 tests pass, 0 fail**
(157 from Milestones 0–7, unchanged, + 46 new).

**Superseded by Milestone 9.** The specific figures below reflect
Milestone 8's original simulation, which did not yet model Milestone 6's
real retry_payment-only execution coverage or Milestone 7's UNKNOWN
outcome. Milestone 9 closed both gaps, which changed RevGuard's specific
numbers for the same seed (more conservative, since some previously
"executed" actions are now correctly recorded as unsupported or
ambiguous). See the Milestone 9 section below for the current
methodology and current numbers; the figures immediately below are kept
as a historical record of what Milestone 8 actually measured at the
time, not as current results.

**Actual evaluation results (synthetic, seed 12345, 1000 opportunities —
recorded here verbatim from a real run, not fabricated or tuned to
produce a particular outcome):**

```
Revenue At Risk:               251,664,828 minor units (INR)
Potentially Recoverable:        79,624,948 minor units (INR)

Strategy       Recovered   Actions  Blocked  Escalated   NetValue   Unnecessary  Rate%   AvgAttempts
fixed_retry     48,345,280     506      494        0     47,453,814    326      19.21%    1.50
static_rules     1,784,269      38      962        0      1,754,964     23       0.71%    1.00
revguard            487,653      34      648      318        464,389     26       0.19%    1.32

vs fixed_retry:  incremental_recovered_revenue=-47,857,627  incremental_net_value=-46,989,425  action_reduction=93.28%
vs static_rules: incremental_recovered_revenue= -1,296,616  incremental_net_value= -1,290,575  action_reduction=10.53%
```

**Honest interpretation, not spun:** under `DefaultPolicyConfig`'s
illustrative Milestone 5 defaults (`MaxAutoAmountMinorUnits` = INR
1,000.00) against this dataset's illustrative amount range (INR
50–5,000, so ~81% of opportunities exceed the auto-approval ceiling),
RevGuard escalates the large majority of opportunities to (unmodeled)
human review rather than acting automatically. This is a genuine,
computed consequence of the real policy thresholds — not a bug, not
tuned, and not hidden: on raw `Revenue Recovered`, RevGuard's automated
actions recover *less* than the fixed-retry baseline in this specific
configuration, while taking 93% fewer automated actions and escalating
318 cases for human judgment instead of blindly acting on them (fixed
retry: 0 escalations, always acts up to 3 attempts regardless of amount
or risk). Rule 15 of this milestone's brief ("do not hard-code an
outcome... the system must calculate the result") is why this number is
reported as computed rather than adjusted — see
[`docs/architecture/evaluation-engine.md`](./docs/architecture/evaluation-engine.md#known-limitations)
for the full discussion of the amount-distribution-vs-policy-threshold
interaction and how a different (e.g. lower-amount-skewed) dataset or a
different `PolicyConfig` would change this comparison.

**Repeatability verification:** ran
`go run ./cmd/evaluate --seed 12345 --cases 1000 --output evaluation.json`
twice into two separate files and `diff`'d them — byte-for-byte
identical. `TestRunEvaluation_Reproducible` additionally confirms this
via `reflect.DeepEqual` and a JSON-string comparison in the automated
suite.

**Verification performed:**
- `gofmt -l .` — clean (no files listed).
- `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass, including every new `evaluation_*_test.go` test (they need no
  database at all); DB-gated M0–M7 tests skip cleanly, as always.
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v`: **203 tests pass, 0 fail** (157 Milestone 0–7 +
  46 new Milestone 8), across `internal/domain`, `internal/http`,
  `internal/repository`, `internal/service`.
- `TEST_DATABASE_URL=... go test ./... -race` — clean, no data races.
- `ai-service/`: confirmed zero git changes (`git status --short
  ai-service/` empty) — Milestone 8 is Go-only, per the "Python tests
  still pass if relevant" checklist item, there is nothing to re-run.
- Manual CLI run: `go run ./cmd/evaluate --seed 12345 --cases 1000`
  produced the table and JSON recorded above; a second run with
  identical arguments produced a byte-identical JSON file (`diff`
  reported no differences).
- Migrations: none added this milestone (no schema change — this
  milestone is entirely in-process, no persistence).
- Docker's Postgres container remains non-functional in this sandbox —
  unchanged, pre-existing limitation since Milestone 0, irrelevant to
  this milestone's verification since M8 needs no database at all; the
  native Postgres instance was used only to re-run the pre-existing
  M0–M7 regression suite.

**Known limitations:** see
[`docs/architecture/evaluation-engine.md`](./docs/architecture/evaluation-engine.md#known-limitations)
for the full list — in summary: the AI diagnosis step is a deterministic
rule-based stand-in (mirroring `ai-service`'s own `MockProvider`), not a
live LLM call; the ground-truth model is an illustrative, uncalibrated
assumption (RevGuard has no historical outcome data yet to calibrate
against, same root cause as Milestone 4's own documented limitation);
`ExecutionEngine`/`WebhookProcessor`/`ReconciliationEngine` are not
invoked (the ground-truth model stands in for what M6/M7 would
determine, since those engines require a live database/provider and
this milestone is explicitly offline); `ESCALATE` never recovers
revenue in this simulation (no human-approval workflow is modeled);
currency is INR-only; the synthetic amount distribution and
`DefaultPolicyConfig`'s auto-approval ceiling interact in a way that
suppresses RevGuard's automated action count relative to the baselines
(discussed in detail above and in the architecture doc).

**Confirmations:**
- Results are SYNTHETIC. No real Razorpay, merchant, or customer data
  was used anywhere in this milestone.
- No claim is made, anywhere in code, tests, or this document, that
  RevGuard has been validated against live Razorpay production data.
- Milestone 9 was NOT started — no file outside the list above was
  created or modified.
- No `git add`/`git commit`/`git push` was performed; the user manages
  git manually in this project, as in every prior milestone.

### Milestone 9 — Evaluation & Benchmarking Refinement: COMPLETE

Goal: close two real production-fidelity gaps in Milestone 8's
evaluation harness so RevGuard's simulated strategy matches production
behavior more closely, add the metrics/report deliverables the
milestone brief specifically required, and re-run the evaluation
honestly — without touching M0–M8's actual production code, the
PolicyEngine's formulas, the state machine, or M7's financial-truth
logic. **Every dataset and every result in this milestone remains
SYNTHETIC**, exactly as in Milestone 8.

**Why this milestone is mostly refinement, not a rebuild:** inspecting
the repository at the start of this milestone showed Milestone 8 had
already built the entire evaluation architecture the brief describes
(deterministic synthetic dataset, two independent baselines, a RevGuard
strategy reusing the real economic/policy formulas, JSON + table output,
fairness tests, architecture doc). Two real gaps remained between that
simulation and actual production behavior — see below — plus report/
metric deliverables (`Expected Recovery Value`, `Incremental Recovery
Rate`, a Markdown report) the M9 brief asked for explicitly that M8
hadn't produced. This milestone closed exactly those gaps rather than
re-deriving the architecture from scratch.

- [x] **Gap 1 — execution-capability fidelity.** Milestone 8's
      simulation treated any policy `ALLOW` as if it executed. In
      production, Milestone 6's `ExecutionEngine` only ever implemented
      `retry_payment` — every other authorized action is rejected with
      `ErrActionNotExecutable` before any side effect (a real, already-
      documented Milestone 6 limitation). Fixed via
      `isRevGuardActionExecutable` (`evaluation_strategies.go`), which
      mirrors `ExecutionEngine.phase1`'s exact check. `StrategyDecision`
      gained `Executed bool`: an `ALLOW` for `retry_payment` sets
      `Executed = true` with real cost/risk; an `ALLOW` for anything else
      (e.g. `send_payment_link`, which genuinely reaches `ALLOW` under
      `DefaultPolicyConfig` — confirmed in Milestone 5's own
      verification) stays authorized but `Executed = false`, zero cost,
      zero possible recovery. Applies only to `RevGuardStrategy` — the
      baselines don't route through RevGuard's `ExecutionEngine` and
      aren't bound by its current implementation coverage.
- [x] **Gap 2 — ambiguous/UNKNOWN financial outcomes.** Milestone 8's
      ground truth was strictly binary (recoverable or not), with no
      equivalent of Milestone 6/7's real UNKNOWN outcome (provider
      timeout at execution time, or an unresolved reconciliation
      lookup — never guessed into SUCCESS/FAILED). Fixed via
      `groundTruthResult.ObservationAmbiguous` (`evaluation_ground_truth.go`,
      independent deterministic draw, illustrative 4% rate,
      `groundTruthAmbiguousRateBps`) and `resolveFinancialOutcome`
      (`evaluation_metrics.go`), which reuses `domain.RecoveryOutcomeStatus`'s
      exact `SUCCESS`/`FAILED`/`UNKNOWN` vocabulary (Milestone 1/7)
      rather than inventing a parallel one.
- [x] **New metrics** (`StrategyMetrics`, `ComparisonResult`):
      `ExpectedRecoveryValueMinorUnits` (RevGuard's Economic Engine's
      ex-ante prediction, `calculateExpectedGrossRecovery`, recorded for
      every `ALLOW` regardless of execution capability — always 0 for
      the baselines, which have no economic model), `UnsupportedActions`
      (count of `ALLOW`-but-not-`Executed`), `AmbiguousOutcomes` (count
      of `Executed`-but-`UNKNOWN`), and `IncrementalRecoveryRate`
      (`revguard.RecoveryRate - baseline.RecoveryRate`, distinct from
      the absolute `IncrementalRecoveredRevenueMinorUnits`).
      `UnnecessaryActions` was narrowed to mean `Executed AND
      resolveFinancialOutcome == FAILED` specifically — an unresolved
      (`UNKNOWN`) executed action is no longer conflated with a
      definitively wasted one. Every opportunity now lands in exactly
      one of six mutually-exclusive buckets (blocked / escalated /
      unsupported / recovered / unnecessary / ambiguous) — see
      `TestAggregateStrategyMetrics_NoDoubleCounting`.
- [x] **Markdown report** (`FormatMarkdownReport`,
      `backend/internal/service/evaluation_engine.go`): generated
      sections — run metadata (generated-at timestamp, `--commit` code/
      version identifier, seed, scenario count, dataset type),
      assumptions, strategy definitions, a full metrics table, the
      RevGuard-vs-baseline comparison table, an interpretation
      paragraph, limitations, and the mandatory "Synthetic evaluation —
      not production performance" label. **Deliberately not part of
      `EvaluationResult`**: the timestamp and commit hash are supplied
      by the caller and rendered only at report-generation time, so
      `EvaluationResult`'s own determinism guarantee (same seed/cases ->
      byte-identical JSON) is unaffected — verified by running the CLI
      twice with different `--commit` values and diffing the JSON output
      (identical) while the Markdown differs only in its metadata line.
- [x] **CLI** (`backend/cmd/evaluate/main.go`) gained `--markdown-output`
      (write the Markdown report to a file, else print to stdout) and
      `--commit` (optional caller-supplied label, e.g. from `git
      rev-parse --short HEAD` — the binary itself never invokes git).
      `--seed`/`--cases`/`--output` are unchanged from Milestone 8.
- [x] **No second RevGuard implementation, no production code touched.**
      `PolicyEngine`, the state machine, `EconomicEngine`,
      `WebhookProcessor`, `ReconciliationEngine`, and every M0–M8 file
      outside `backend/internal/service/evaluation_*.go`,
      `backend/cmd/evaluate/main.go`, and this document are unmodified —
      confirmed via `git diff --stat` against the Milestone 8 commit
      showing changes contained to exactly those files (plus the
      architecture doc).
- [x] **Dashboard integration: none, by design.** The frontend
      (`frontend/`) was inspected and confirmed still at its Milestone 0
      skeleton state (no analytics/evaluation presentation hooks of any
      kind — `git grep` for evaluation/benchmark-related terms in
      `frontend/` returns nothing outside build artifacts). Per this
      milestone's explicit instruction ("if M8 already has hooks,
      integrate; otherwise do NOT redesign the dashboard"), no frontend
      file was created, modified, or even inspected beyond this check.
- [x] Explicitly NOT implemented (by design, per milestone scope): new
      AI models, ML training, production automatic retries, new payment
      flows, live Razorpay payment execution, webhook/reconciliation/
      policy redesign, human approval UI, Kubernetes, Temporal, new
      databases/message brokers, unnecessary microservices, production
      load-testing infrastructure, unrelated frontend redesign, any M10
      work.

**Test counts:** 11 new tests added on top of Milestone 8's 46
(executability-gating tests for `RevGuardStrategy`, ambiguous-outcome
determinism/rate/no-double-counting/no-recovery tests for the new
ground-truth field and metrics, `ExpectedRecoveryValue`/
`IncrementalRecoveryRate` formula tests, and two `FormatMarkdownReport`
tests) — **57 evaluation-specific tests total.** Combined with the
unchanged Milestone 0–7 suite (157), the full `go test ./...` (with
`TEST_DATABASE_URL` set) run is **214 tests pass, 0 fail**.

**Actual evaluation results (synthetic, seed 12345, 1000 opportunities,
current methodology — recorded here verbatim from a real run):**

```
Revenue At Risk:               251,664,828 minor units (INR)
Potentially Recoverable:        79,624,948 minor units (INR)

Strategy       Recovered   Actions  Blocked  Escalated  Unsupported  Ambiguous   NetValue   Unnecessary  Rate%   AvgAttempts
fixed_retry     47,563,828     506      494        0            0         15    46,672,362      315     18.90%    1.50
static_rules     1,746,191      38      962        0            0          2     1,716,886       22      0.69%    1.00
revguard            351,570      26      648      318            8          1       331,183       19      0.14%    1.27

vs fixed_retry:  incremental_recovered_revenue=-47,212,258  incremental_net_value=-46,341,179  action_reduction=94.86%  incremental_recovery_rate=-0.1876
vs static_rules: incremental_recovered_revenue= -1,394,621  incremental_net_value= -1,385,703  action_reduction=31.58%  incremental_recovery_rate=-0.0055

revguard ExpectedRecoveryValueMinorUnits (ex-ante prediction, all ALLOW decisions incl. unsupported): 941,188
```

**Honest interpretation, not spun:** RevGuard's numbers dropped from
Milestone 8's run for the same seed (487,653 -> 351,570 recovered; 34 ->
26 actions taken) precisely because the two fidelity fixes are more
conservative: 8 of RevGuard's `ALLOW` decisions were `send_payment_link`
(authorized, but not currently executable per Milestone 6 — recorded as
`UnsupportedActions`, not silently credited), and 1 executed action came
back `UNKNOWN` rather than being guessed into success. This is the
intended, honest effect of aligning the simulation with the real
system's current implementation coverage, not a regression or a result
that was tuned to look worse. RevGuard's raw `Revenue Recovered` remains
below both baselines' in this specific configuration (same root cause
identified in Milestone 8: `DefaultPolicyConfig.MaxAutoAmountMinorUnits`
= INR 1,000 vs. this dataset's INR 50–5,000 amount range, so most
opportunities escalate to unmodeled human review), while taking 94.86%
and 31.58% fewer actions respectively than the two baselines. Separately,
RevGuard's `ExpectedRecoveryValueMinorUnits` (941,188 — the Economic
Engine's ex-ante prediction across all its `ALLOW` decisions, including
the 8 unsupported ones) is markedly higher than its realized
`RevenueRecoveredMinorUnits` (351,570): part of that gap is the
independently-tuned, deliberately-imperfect ground-truth model (see
Milestone 8's "why not reuse RevGuard's own base-rate table" rationale),
and part is that the Economic Engine's prediction is computed before
Milestone 6's execution-capability check, so it includes revenue the
system cannot currently act on at all — a genuine, calibration-relevant
finding this evaluation surfaces rather than hides.

**Repeatability verification:** ran
`go run ./cmd/evaluate --seed 12345 --cases 1000 --output evaluation.json`
twice (once with `--commit abc`, once with a different value) and
`diff`'d the two JSON files — byte-for-byte identical, confirming the
new `--commit`/Markdown-report parameters have zero effect on the
deterministic result. `TestRunEvaluation_Reproducible` continues to
verify this via `reflect.DeepEqual` and a JSON-string comparison in the
automated suite.

**Verification performed:**
- `gofmt -l .` — clean (no files listed).
- `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass, including all 57 evaluation tests (none need a database).
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v`: **214 tests pass, 0 fail** (157 Milestone 0–7 +
  57 Milestone 8–9 evaluation tests), across `internal/domain`,
  `internal/http`, `internal/repository`, `internal/service`.
- `TEST_DATABASE_URL=... go test ./... -race` — clean, no data races.
- `ai-service/`: zero changes — Milestone 9 is Go-only, same as
  Milestone 8.
- Manual CLI run: `go run ./cmd/evaluate --seed 12345 --cases 1000
  --output evaluation.json --markdown-output evaluation.md --commit
  8b42747` produced the table, JSON, and Markdown report recorded above
  and in `docs/architecture/evaluation-engine.md`; a second run with
  identical `--seed`/`--cases` (different `--commit`) produced a
  byte-identical JSON file (`diff` reported no differences) and a
  Markdown report differing only in its metadata line, as expected.
- No output files (`evaluation.json`/`evaluation.md`) were left in the
  repository working tree — both were generated into a scratch
  directory outside the repo for verification purposes only, consistent
  with "no temporary/debug artifacts" and Milestone 4–7's precedent of
  deleting one-off verification tools after use.
- Frontend: confirmed no dashboard/evaluation hooks exist; no frontend
  file was touched.
- Migrations: none added — this milestone changes no schema, same as
  Milestone 8.

**Known limitations:** all of Milestone 8's limitations still apply
(deterministic AI-diagnosis stand-in, uncalibrated ground-truth model,
INR-only, the amount-distribution-vs-policy-threshold interaction) —
see the architecture doc's "Known limitations" for the full, current
list. Two are newly added this milestone: the real execution/webhook/
reconciliation *code paths* are still not invoked (only their
documented behavioral contracts are modeled); the 4% ambiguous-outcome
rate is an illustrative assumption, not a measured Razorpay reliability
figure.

**Confirmations:**
- Results are SYNTHETIC. No real Razorpay, merchant, or customer data
  was used anywhere in this milestone.
- No claim is made, anywhere in code, tests, or this document, that
  RevGuard has been validated against live Razorpay production data.
- No real Razorpay financial action was executed — this milestone opens
  no network connection and calls no external API at all.
- Milestone 10 was NOT started — no file outside the list above was
  created or modified.
- No `git init`/`git add`/`git commit`/`git push`/branch-creation was
  performed by this work; the user manages git manually in this
  project, as in every prior milestone. (A commit titled "working on
  dashboard" appears in this session's `git log` — that was made by the
  user directly, not by this work, and touched no file this milestone
  didn't already account for.)

**Recommended next milestone:** Milestone 10 — not yet scoped. Natural
candidates surfaced by this milestone's honest results (not a
commitment, just observations for whoever scopes it next): (a)
extending `ExecutionEngine` to cover more than `retry_payment`, which
this evaluation shows leaves real predicted value
(`ExpectedRecoveryValueMinorUnits`) uncaptured; (b) revisiting whether
`DefaultPolicyConfig.MaxAutoAmountMinorUnits` should scale with a
merchant's actual payment-amount distribution rather than one fixed
illustrative ceiling; (c) a genuinely presentational (read-only)
dashboard surface for evaluation results, once there is real interest in
making this data visible in the frontend. Do not begin implementation
until explicitly instructed.

### Milestone 10 — Recovery Optimization & Production Readiness: COMPLETE

Goal: use Milestone 9's own evaluation findings to make two genuine
product improvements — real `send_payment_link` execution and
merchant-selectable policy risk profiles — without weakening any safety
control, then re-run the exact same evaluation methodology to measure
the honest trade-off. **Every evaluation number in this section remains
SYNTHETIC**, exactly as in Milestones 8–9; nothing here is, or claims to
be, live Razorpay production data.

**Note on the incoming brief's milestone labels:** the M10 instructions
described "M8 → dashboard" and "M9 → reproducible synthetic evaluation."
Per this repository's actual history (this document), M8 is the
evaluation/revenue-recovery-proof harness and M9 is its execution-
fidelity refinement — no dashboard existed before this milestone. This
section follows the repository's real history, not the brief's
mislabeling, per the brief's own instruction to inspect the repository
first and not let the prompt override it.

- [x] **Phase 1 — repository inspection.** Read CLAUDE.md's M5–M9
      sections, `execution_engine.go`, `policy_engine.go`/`policy_config.go`,
      `webhook_processor.go`/`reconciliation_engine.go`, migrations
      (confirmed `recovery_actions.action_type`'s `CHECK` constraint
      already allowed `SEND_PAYMENT_LINK` since migration `000006` —
      **no new migration was needed**), the existing `PaymentProvider`
      abstraction, `execution_engine_test.go`'s existing
      `seedAllowDecisionForSendPaymentLink` fixture, and confirmed via
      `find`/`git grep` that the frontend was still the Milestone 0
      skeleton with zero evaluation/dashboard hooks.
- [x] **Phase 2 — `send_payment_link` execution**
      (`backend/internal/service/{payment_provider,fake_payment_provider,
      razorpay_provider,execution_engine}.go`): `PaymentProvider` gained
      `SendPaymentLink(ctx, SendPaymentLinkRequest) (SendPaymentLinkResult, error)`
      with the identical error-vs-result (ambiguous-vs-definitive) split
      as `RetryPayment`. `FakeProvider.SendPaymentLink` applies the same
      five deterministic scenarios. `RazorpayProvider.SendPaymentLink`
      calls the identical Payment Links operation as `RetryPayment`
      through a new shared private `createPaymentLink` helper (no HTTP
      logic duplicated). `ExecutionEngine`'s hardcoded
      `AuthorizedAction != retry_payment` check became a lookup into a
      new `executableActions` map (`{retry_payment: RETRY_PAYMENT,
      send_payment_link: SEND_PAYMENT_LINK}`); `Execute`'s Phase 2
      dispatches to the matching provider method by
      `action.ActionType`, then normalizes the result before handing it
      to **Phase 3, which was not touched at all**. Because Phase 3
      already (since Milestone 6) transitions `RecoveryCase.Status`
      unconditionally to `VERIFYING` — never directly to `SUCCESS` —
      regardless of the provider's `Succeeded` value, "payment-link
      creation is not financial success" holds for `send_payment_link`
      automatically, with zero new safety logic required. Financial
      `SUCCESS` still requires Milestone 7's webhook/reconciliation, via
      the unmodified `applyFinancialOutcome`.
- [x] **Provider safety confirmed**: `FakeProvider` distinguishes "link
      created" (`Succeeded=true`) from "payment succeeded" exactly as it
      already did for `retry_payment` — a `SUCCEEDED` `RecoveryAction`
      status is never conflated with a `SUCCESS` `RecoveryOutcome`
      anywhere in the code. `RazorpayProvider.SendPaymentLink` reuses
      the same **NOT VERIFIED against a real Razorpay account** status
      as `RetryPayment` — see below.
- [x] **Phase 3 — policy profiles** (`backend/internal/service/policy_config.go`):
      `ConservativePolicyConfig`, `BalancedPolicyConfig` (numerically
      identical to the pre-existing `DefaultPolicyConfig` — kept as a
      separate variable, not an alias, so no M0–M9 wiring or test had to
      change), and `AggressivePolicyConfig`, registered in
      `PolicyProfiles map[string]PolicyConfig`. Exact values and
      rationale are documented in
      [`docs/architecture/policy-engine.md`](./docs/architecture/policy-engine.md#milestone-10-policy-profiles).
      `evaluatePolicyRules` itself (Milestone 5) is **byte-for-byte
      unchanged** — only threshold values differ between profiles.
      `stop_recovery` is unconditionally `BLOCK`ed by rule (B)
      regardless of any profile's `AutoAllowedActions` map (enforced in
      code, not config, so no profile can weaken it), and
      `MinimumExpectedIncrementalValueMinorUnits` is never negative in
      any profile — "aggressive" means more tolerant thresholds, never
      a weakened safety rule. `POLICY_PROFILE` (env var, default
      `"balanced"`) is wired into `cmd/server/main.go`, looked up in
      `service.PolicyProfiles`, failing fast on an unrecognized value.
- [x] **Phase 4/5 — multi-profile evaluation with execution fidelity**
      (`backend/internal/service/evaluation_{strategies,engine}.go`):
      `RevGuardStrategy` gained a `policyConfig` field and
      `NewRevGuardStrategyWithProfile(name, config)`.
      `isRevGuardActionExecutable` now consults the exact same
      `executableActions` map `ExecutionEngine` uses (not a separate,
      evaluation-only list), so `send_payment_link` is now credited with
      real cost/possible recovery in the simulation, matching its new
      production capability. `RunEvaluation` runs five strategies
      (`fixed_retry`, `static_rules`, `revguard_conservative`,
      `revguard_balanced`, `revguard_aggressive`) against the **same
      dataset** for a given `(seed, cases)` and produces a 3×2
      comparison matrix (every profile against every baseline).
      **The synthetic dataset generator and ground-truth model were not
      modified this milestone** — confirmed via
      `git diff --stat` against the Milestone 9 baseline showing
      `evaluation_dataset.go`/`evaluation_ground_truth.go` untouched.
- [x] **Phase 6 — economic engine inspected, not rewritten.**
      `HeuristicProbabilityEstimator`, `GetActionEconomics`, and the
      economic formulas (`economic_calculations.go`) were read in full
      and found to have no structural defect warranting a change — the
      calibration gap Milestone 9 observed
      (`ExpectedRecoveryValueMinorUnits` exceeding
      `RevenueRecoveredMinorUnits`) is fully explained by (a) the
      deliberately-independent ground-truth model and (b) the
      execution-capability/ambiguous-outcome gaps Milestone 9 itself
      already introduced metrics for. No probability, cost, or risk
      value in `economic_calculations.go`, `action_economics.go`, or
      `recovery_probability_estimator.go` was changed. AI confidence is
      still never treated as recovery probability, and expected
      (predicted) recovery is still never conflated with actual
      (realized) recovery anywhere in the codebase.
- [x] **Phase 9 — dashboard.** No dashboard existed (confirmed above).
      Added the minimum read-only presentation: `GET /v1/evaluation`
      (`backend/internal/http/evaluation.go`, wired in `router.go`) —
      calls `service.RunEvaluation` directly, opens no database
      connection, has no side effects, bounds `?cases=` at 5,000 to
      prevent an unbounded computation from an unauthenticated query
      parameter — and one new page,
      `frontend/app/evaluation/page.tsx`, a client component that
      fetches that endpoint via `NEXT_PUBLIC_API_URL` (the env var
      already defined in `.env.example`) and renders two plain HTML
      tables (strategy comparison; profile-vs-baseline). No chart
      library or other dependency was added; no number is hardcoded in
      the component; the homepage (`app/page.tsx`) was not touched.
- [x] Explicitly NOT implemented (by design, per milestone scope): ML
      training, new databases, Redpanda/Kafka redesign, Kubernetes,
      Temporal, new microservices, automatic background retry workers,
      customer messaging integrations, live payment experimentation,
      frontend redesign beyond the one new page, policy bypass of any
      kind, fabricated benchmark improvements, any M11 work.

**Test counts:** 5 new tests in `internal/http` (`evaluation_test.go`:
defaults, custom seed/cases, invalid seed, cases-above-max, negative
cases), plus new/updated tests in `internal/service` across
`execution_engine_test.go` (6 new `SendPaymentLink_*` tests mirroring
every `retry_payment` test, plus the `UnsupportedActionNoProviderCall`
fixture switched to `send_reminder`), `fake_payment_provider_test.go`
(5 new `SendPaymentLink_*` scenario tests), `policy_profiles_test.go`
(new file, 9 tests: registry completeness, `Balanced == Default`,
per-profile threshold validity, `stop_recovery` always `BLOCK`ed,
negative expected value always `BLOCK`ed, confidence-alone/expected-
value-alone never authorize, genuinely different outcomes across
profiles on identical input, determinism), and `evaluation_strategies_test.go`/
`evaluation_engine_test.go` (send_payment_link-is-now-executed test,
updated unsupported-action test, updated multi-profile
`RunEvaluation` tests, two new reproducibility/differentiation tests).
Combined with the unchanged Milestone 0–9 suite, the full `go test ./...`
(with `TEST_DATABASE_URL` set) run is **242 tests pass, 0 fail**.

**Actual evaluation results (synthetic, seed 12345, 1000 opportunities —
recorded here verbatim from a real run):**

```
Revenue At Risk:               251,664,828 minor units (INR)
Potentially Recoverable:        79,624,948 minor units (INR)

Strategy                Recovered  Actions  Blocked  Escalated  Unsupported  Ambiguous  NetValue    Unnecessary  Rate%   AvgAttempts
fixed_retry              47,563,828    506      494        0            0        15    46,672,362      315     18.90%   1.50
static_rules               1,746,191     38      962        0            0         2     1,716,886       22      0.69%   1.00
revguard_conservative               0      1      827      172            0         1          -314        0      0.00%   1.00
revguard_balanced             449,575     34      648      318            0         3       426,311       24      0.18%   1.32
revguard_aggressive         6,931,542    128      528      252           92         6     6,809,303       77      2.75%   1.68

Comparison (each RevGuard profile vs. each baseline, same dataset):
revguard_conservative vs fixed_retry:   incremental_recovered_revenue=-47,563,828  incremental_net_value=-46,672,676  action_reduction=99.80%   incremental_recovery_rate=-0.1890
revguard_conservative vs static_rules:  incremental_recovered_revenue= -1,746,191  incremental_net_value= -1,717,200  action_reduction=97.37%   incremental_recovery_rate=-0.0069
revguard_balanced      vs fixed_retry:  incremental_recovered_revenue=-47,114,253  incremental_net_value=-46,246,051  action_reduction=93.28%   incremental_recovery_rate=-0.1872
revguard_balanced      vs static_rules: incremental_recovered_revenue= -1,296,616  incremental_net_value= -1,290,575  action_reduction=10.53%   incremental_recovery_rate=-0.0052
revguard_aggressive    vs fixed_retry:  incremental_recovered_revenue=-40,632,286  incremental_net_value=-39,863,059  action_reduction=74.70%   incremental_recovery_rate=-0.1615
revguard_aggressive    vs static_rules: incremental_recovered_revenue=  5,185,351  incremental_net_value=  5,092,417  action_reduction=-236.84% incremental_recovery_rate= 0.0206
```

**Honest interpretation — the actual trade-off exposed, not a
"RevGuard wins" narrative:**
- **Conservative recovers essentially nothing (0)** in this dataset: its
  ₹500 auto-approval ceiling and required positive expected-value buffer
  escalate or block nearly everything (827 blocked + 172 escalated out
  of 1000). This is what "minimize unnecessary actions" looks like taken
  to its logical extreme against a dataset whose amounts mostly exceed
  ₹500 — it is not a bug, it is the conservative profile doing exactly
  what it's configured to do.
- **Aggressive dominates the other two RevGuard profiles by a wide
  margin** (6.93M recovered vs. 450K balanced vs. 0 conservative) and is
  the **only RevGuard profile that beats a baseline outright**: it
  recovers 5,185,351 more than `static_rules` with a positive
  `incremental_recovery_rate` (+0.0206), while still taking 74.70% fewer
  actions than `fixed_retry`. It still recovers less than `fixed_retry`
  in raw terms (-40.6M) — `fixed_retry` blindly retries almost
  everything, including many opportunities aggressive correctly
  escalates or that turn out ambiguous/unsupported.
- **None of the three profiles beats `fixed_retry` on raw recovered
  revenue** in this specific illustrative dataset (INR 50–5,000 amount
  range against illustrative auto-approval ceilings of ₹500/₹1,000/₹3,000).
  This is the same root cause Milestone 8/9 already identified and is
  reported here unchanged, per the explicit instruction not to force a
  "RevGuard wins" conclusion: a merchant with a real payment-amount
  distribution skewed lower (or a profile with a higher ceiling than
  even "aggressive" here) would likely see a different outcome — that is
  exactly the kind of trade-off exploration these profiles now make
  possible, and exactly what future tuning should be informed by real
  data for, not this synthetic run.
- `revguard_aggressive` is also the only profile with a nonzero
  `UnsupportedActions` count (92) — it auto-allows
  `request_payment_method_change`, which has no execution
  implementation, so those opportunities are correctly recorded as
  authorized-but-not-executed rather than silently credited.

**Repeatability verification:** ran
`go run ./cmd/evaluate --seed 12345 --cases 1000 --output evaluation.json`
twice with different `--commit` values and `diff`'d the two JSON files —
byte-for-byte identical.
`TestRunEvaluation_ProfilesReproducibleOnSameDatasetAndSeed` verifies
this per-profile (not just for the whole result) in the automated suite.

**Verification performed:**
- `gofmt -l .` — clean.
- `go build ./...`, `go vet ./...` — clean.
- `go test ./...` with no `TEST_DATABASE_URL` — all non-DB-gated tests
  pass.
- `TEST_DATABASE_URL=postgres://revguard:revguard@localhost:5432/revguard?sslmode=disable
  go test ./... -v`: **242 tests pass, 0 fail**, across `internal/domain`,
  `internal/http`, `internal/repository`, `internal/service`.
- `TEST_DATABASE_URL=... go test ./... -race` — clean, no data races.
- `ai-service/`: zero changes — Milestone 10 is Go/TypeScript-only.
- Migrations: none added — `recovery_actions.action_type`'s existing
  `CHECK` constraint (migration `000006`) already allowed
  `SEND_PAYMENT_LINK` since Milestone 1.
- Manual end-to-end smoke test: started the real Go backend
  (`PAYMENT_PROVIDER=fake`, native Postgres) and confirmed
  `GET /health` and `GET /v1/evaluation?seed=1&cases=20` both return
  real, correctly-shaped JSON. Started the Next.js dev server with
  `NEXT_PUBLIC_API_URL` pointed at that backend and confirmed
  `GET /evaluation` returns `200` with the expected static shell
  (`RevGuard Evaluation` header, `Synthetic evaluation` badge, `Loading…`
  placeholder) server-rendered before client-side hydration; `npm run
  build` and `npx tsc --noEmit` both pass. **Limitation honestly noted:**
  this sandbox has no headless browser available, so the client-side
  fetch-and-render of live data was not visually confirmed in an actual
  browser — it was verified indirectly by (a) confirming the exact JSON
  shape the component expects via a direct `curl` against the running
  backend, matching the TypeScript types field-for-field, and (b) `npx
  tsc --noEmit` type-checking the component against those exact types.
  Both temporary server processes were killed after verification; no
  server was left running.
- Read-only production readiness audit (Phase 10), verified by direct
  code inspection, not assumption: AI cannot execute actions or bypass
  policy (unchanged since M3/M5; `deterministicDiagnosis` in the
  evaluation harness is a labeled stand-in, never live AI, and never
  executes anything real either); a client cannot choose an arbitrary
  action (`POST /execute` and the new `GET /v1/evaluation` both take no
  action-selecting parameter; `ExecutionEngine` always reloads
  `AuthorizedAction` fresh from PostgreSQL); `BLOCK`/`ESCALATE` cannot
  execute (`ErrPolicyDecisionNotAllow`, tested); execution requires a
  persisted `ALLOW` (`phase1` loads the decision by ID, never trusts a
  caller-supplied action); provider calls happen outside DB transactions
  (Phase 2 unchanged, confirmed for both `RetryPayment` and the new
  `SendPaymentLink` dispatch); ambiguous outcomes are never treated as
  success (`providerErr != nil` branch in `phase3` unchanged;
  `resolveFinancialOutcome` in the evaluation harness checks
  `ObservationAmbiguous` before `Recoverable`); webhooks/reconciliation
  remain the sole authority for financial truth (`webhook_processor.go`/
  `reconciliation_engine.go` have zero diff this milestone); money stays
  integer minor units everywhere new (`SendPaymentLinkRequest/Result`,
  every `PolicyConfig` profile); idempotency is intact
  (`send_payment_link` reuses the identical
  `"policy-decision:<policyDecisionID>"` key and `TryCreate`/
  `resumeExisting` logic, verified by dedicated duplicate/concurrency
  tests); the audit trail is intact (`phase1`/`phase3` audit code is
  untouched and already action-type-generic); no secret is logged
  (`RazorpayProvider`'s new `createPaymentLink` helper reuses the exact
  same `SetBasicAuth`-only credential path as before,
  `TestExecutionEngine_NoSecretsInPersistedMetadata` still passes); no
  real Razorpay financial action was executed (no
  `RAZORPAY_KEY_ID`/`RAZORPAY_KEY_SECRET` configured, `RazorpayProvider`
  was never invoked outside of code review — only `FakeProvider` ran in
  every test and smoke test); no Milestone 11 work was started.

**Dashboard integration status:** minimal, read-only, real-data-backed —
see Phase 9 above. Not a redesign; the existing homepage is untouched.

**Known limitations:** all Milestone 8/9 limitations still apply (see
`docs/architecture/evaluation-engine.md`'s "Known limitations"). New
this milestone: `RazorpayProvider.SendPaymentLink` is **NOT VERIFIED**
against a real Razorpay account, for the identical reason
`RazorpayProvider.RetryPayment` never was (no credentials, no verified
network access to Razorpay's API/docs in this sandbox) — it reuses the
same unverified HTTP call path; the three policy profiles' exact
threshold values are illustrative choices for this demonstration, not
derived from any merchant's real payment-amount distribution or
historical loss data (the honest interpretation above explains exactly
how that shows up in the aggressive-vs-fixed_retry result); the
frontend's live data-fetch was not visually confirmed in a real browser
(see "Verification performed" above for exactly what was and wasn't
checked).

**Confirmations:**
- Results are SYNTHETIC. No real Razorpay, merchant, or customer data
  was used anywhere in this milestone.
- No claim is made that RevGuard has been validated against live
  Razorpay production data, or that `send_payment_link` execution has
  been verified against a real Razorpay account.
- No real Razorpay financial action was executed — `RazorpayProvider`
  was never invoked against a live endpoint at any point this milestone.
- Milestone 11 was NOT started — no file outside the scope described
  above was created or modified.
- No `git init`/`git add`/`git commit`/`git push`/branch-creation was
  performed by this work; the user manages git manually in this
  project, as in every prior milestone. (A commit titled "working on
  dashboard," made by the user directly mid-session, appears in this
  session's `git log` — not created by this work.)

**Recommended next milestone:** Milestone 11 — not yet scoped. Natural
candidates surfaced by this milestone's honest results (observations,
not commitments): (a) calibrating policy-profile thresholds (especially
the auto-approval ceiling) against a real or more representative
payment-amount distribution, since this synthetic dataset's shape is
what makes even the aggressive profile lose to `fixed_retry` on raw
recovered revenue; (b) extending `ExecutionEngine` to a third action
(e.g. `send_reminder`, the cheapest and simplest remaining one) now that
the `executableActions` pattern makes this a small, well-contained
change; (c) if real interest emerges in showing this data more broadly,
a slightly richer (still read-only) dashboard view — e.g. a per-profile
detail page — built on the same `GET /v1/evaluation` endpoint. Do not
begin implementation until explicitly instructed.

**Next milestone: Milestone 11.** Not yet scoped. Do not begin
implementation until explicitly instructed.

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
- Under PostgreSQL READ COMMITTED, sequential `SELECT`s inside one
  transaction do **not** share a single consistent snapshot — a
  concurrent transaction can commit in the gap between two reads in the
  same function. For an idempotency-check-then-state-check pattern (see
  `PolicyEngine.Evaluate` and `ExecutionEngine.phase1`), re-check
  idempotency once more immediately before treating a wrong-state
  observation as a genuine error: if a concurrent call just finished,
  its result is now visible precisely because it already committed, and
  this is a safe retry, not an error. This was a real, reproducible bug
  (found via a ~30%-flaky concurrency test) before the re-check was
  added — don't skip it when writing the next such engine.
- When an external call's outcome can be genuinely ambiguous (a
  provider timeout, a transport error, or discovering an
  abandoned/orphaned attempt from a crashed process), persist a
  dedicated `UNKNOWN`-style status rather than guessing it into a
  success or failure value, and never automatically retry it. Only a
  later, dedicated reconciliation step (Milestone 7) may resolve
  `UNKNOWN` — see `ExecutionEngine`'s provider-timeout handling and
  `docs/architecture/execution-engine.md`.
