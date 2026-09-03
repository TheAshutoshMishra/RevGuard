"""RevGuard AI service entry point.

Milestone 0: infrastructure skeleton only. No LLM integration yet.
"""

from fastapi import FastAPI

app = FastAPI(title="RevGuard AI Service")


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}
