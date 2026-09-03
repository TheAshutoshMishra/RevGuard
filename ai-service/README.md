# RevGuard AI Service

FastAPI service that owns AI/ML/LLM-related intelligence for RevGuard.
It only ever produces recommendations — it never calls infrastructure,
never authorizes payments, and never mutates durable state. See
[docs/architecture/ai-diagnosis.md](../docs/architecture/ai-diagnosis.md)
in the repo root for the full contract.

## Local development

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload --port 8000
```

## Testing

```bash
pip install -r requirements-dev.txt
pytest
```

## Endpoints

- `GET /health` → `{"status": "ok"}`
- `POST /v1/diagnose` → structured diagnosis/recommendation for a
  RecoveryCase. See `app/models/diagnosis.py` for the request/response
  contract and `app/prompts/diagnosis_v1.py` for the system prompt.

## LLM provider

Selected via `AI_PROVIDER` (default `mock`):

- `mock` — deterministic, rule-based, no credentials required. Used
  automatically in tests and local development.
- `anthropic` — real model calls via the Anthropic Messages API. Requires
  `ANTHROPIC_API_KEY` (see `.env.example` at the repo root; never commit a
  real key). Optionally set `ANTHROPIC_MODEL` (defaults to
  `claude-sonnet-5`).

Adding another provider means implementing `app/providers/base.py`'s
`LLMProvider` interface and wiring it into `build_provider()` in
`app/main.py` — no other code changes.
