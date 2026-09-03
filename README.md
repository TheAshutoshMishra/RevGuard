# RevGuard

RevGuard is a revenue-protection system built around a strict separation of
concerns:

> **AI recommends. Policy decides. Infrastructure executes. Webhooks verify.
> Analytics proves.**

## Architecture

| Layer                    | Technology                     |
|---------------------------|---------------------------------|
| Core backend & authority  | Go                              |
| AI / ML / LLM service     | Python + FastAPI                |
| Durable source of truth   | PostgreSQL                      |
| Idempotency / coordination / cache | Redis                  |
| Event streaming           | Redpanda (Kafka API-compatible) |
| Frontend                  | Next.js + TypeScript            |
| Local development         | Docker Compose                  |

The Go backend is the sole authority for decisions and state changes. The
Python AI service only produces recommendations — it never acts directly on
infrastructure. All durable state lives in PostgreSQL; Redis is used purely
for idempotency, coordination, and caching, never as a system of record.

## Repository layout

```
revguard/
├── backend/          # Go core backend (authority, API, domain logic)
│   ├── cmd/server/       # main entrypoint
│   └── internal/
│       ├── config/       # env-based configuration
│       ├── domain/       # domain models (empty until Milestone 1)
│       ├── http/         # HTTP layer (chi router, handlers)
│       ├── infrastructure/ # thin wrappers around Postgres/Redis/Redpanda
│       └── repository/   # persistence layer (empty until Milestone 1)
├── ai-service/       # Python FastAPI service (AI/ML/LLM recommendations)
│   └── app/
├── frontend/         # Next.js + TypeScript frontend
├── deployments/       # deployment configuration (future use)
├── docs/
│   ├── architecture/  # architecture notes/diagrams
│   └── decisions/     # ADRs
├── scripts/          # dev/ops scripts
├── tests/            # cross-service/integration tests
├── docker-compose.yml
├── .env.example
└── CLAUDE.md          # locked architecture + milestone tracker
```

## Milestone 0 — Foundation

This milestone establishes the skeleton for every service with no business
logic:

- Go backend with a `/health` endpoint
- Python FastAPI service with a `/health` endpoint
- Next.js frontend skeleton
- PostgreSQL, Redis, and Redpanda as Docker Compose services
- Dockerfiles for backend, ai-service, and frontend
- `.env.example` and local `.env` for configuration

See [CLAUDE.md](./CLAUDE.md) for the full milestone tracker and locked
architecture decisions.

## Local development

1. Copy the environment template:

   ```bash
   cp .env.example .env
   ```

2. Start the full stack:

   ```bash
   docker compose up -d --build
   ```

3. Apply database migrations:

   ```bash
   cd backend && go run ./cmd/migrate -command up
   ```

4. Check service health:

   ```bash
   curl http://localhost:8080/health   # backend -> {"status":"ok"}
   curl http://localhost:8000/health   # ai-service -> {"status":"ok"}
   open http://localhost:3000          # frontend
   ```

### Running services individually

**Backend (Go)**

```bash
cd backend
go build ./...
go test ./...
go run ./cmd/migrate -command up   # apply migrations (add -command down to roll back)
go run ./cmd/server
```

The domain model lives in `backend/internal/domain` (merchants, customers,
payments, payment attempts, recovery cases/actions/outcomes, recovery
events, audit events) and is persisted via `backend/internal/repository`.
Monetary amounts are always integer minor units + an explicit currency code
(e.g. ₹499.50 → `49950`, `"INR"`) — never floating point. See
[CLAUDE.md](./CLAUDE.md) for the full schema and rationale.

Event ingestion and recovery orchestration live in `backend/internal/service`
and are exposed via `POST /events`:

```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "evt-123",
    "event_type": "payment.failed",
    "aggregate_type": "payment",
    "aggregate_id": "<existing payment UUID>",
    "merchant_id": "<that payment'"'"'s merchant UUID>",
    "occurred_at": "2026-01-01T00:00:00Z",
    "payload": {"reason": "insufficient_funds"}
  }'
```

See [docs/architecture/event-flow.md](./docs/architecture/event-flow.md) for
the full ingestion → idempotency → recovery case → state machine → audit
pipeline, including why PostgreSQL (not Redis) is the durable idempotency
authority.

**AI service (Python)**

```bash
cd ai-service
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload --port 8000
```

When a `RecoveryCase` reaches `ANALYZING`, the backend calls the AI
service's `POST /v1/diagnose` for a structured diagnosis and
recommendation, then moves the case to `ANALYZED`. The AI service only
ever recommends — it never authorizes or executes anything. Defaults to a
deterministic mock provider (`AI_PROVIDER=mock`, no credentials needed);
set `AI_PROVIDER=anthropic` and `ANTHROPIC_API_KEY` for real model calls.
See [docs/architecture/ai-diagnosis.md](./docs/architecture/ai-diagnosis.md)
for the full contract.

**Frontend (Next.js)**

```bash
cd frontend
npm install
npm run dev
```

## Services (Docker Compose)

| Service    | Port(s)                | Purpose                          |
|------------|-------------------------|-----------------------------------|
| postgres   | 5432                    | durable source of truth           |
| redis      | 6379                    | idempotency / coordination / cache |
| redpanda   | 9092, 8081, 8082, 9644  | event streaming (Kafka API)        |
| backend    | 8080                    | Go core backend / API             |
| ai-service | 8000                    | Python FastAPI AI/ML service      |
| frontend   | 3000                    | Next.js UI                        |
