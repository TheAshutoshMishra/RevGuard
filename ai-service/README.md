# RevGuard AI Service

FastAPI service that will eventually own AI/ML/LLM-related intelligence for
RevGuard: diagnosis, structured recommendations, recovery-probability
estimation, and evaluation.

## Milestone 0 scope

This is an infrastructure skeleton only. No LLM provider, prompts, or
recommendation logic has been implemented yet.

## Local development

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --reload --port 8000
```

## Endpoints

- `GET /health` → `{"status": "ok"}`
