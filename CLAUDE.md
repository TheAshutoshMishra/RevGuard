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
  internal/repository/      Postgres persistence layer (Create/GetByID per entity)
  internal/service/         business logic (later milestones)
  migrations/         SQL migrations (golang-migrate up/down pairs, one per table)
ai-service/          Python FastAPI AI/ML/LLM service
  app/main.py         FastAPI app + /health
frontend/            Next.js + TypeScript frontend
deployments/         deployment configuration (future use)
docs/architecture/   architecture notes/diagrams
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

### Milestone 2 — Next milestone: NOT STARTED

Not yet scoped. Do not begin implementation until explicitly instructed.
Likely candidates per the product goal (not a commitment): the recovery
case state machine, AI diagnosis integration point, or event
ingestion/production via Redpanda — to be confirmed with the user before
starting.

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
