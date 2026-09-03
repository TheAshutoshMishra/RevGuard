"""RevGuard AI service entry point.

Milestone 3: recovery diagnosis. This service only ever produces
recommendations — it never calls infrastructure, never authorizes
payments, and never mutates durable state. See docs/architecture/
ai-diagnosis.md in the repo root for the full contract.
"""

from __future__ import annotations

import logging
import os
import time

from fastapi import FastAPI, HTTPException

from app.models.diagnosis import DiagnosisRequest, DiagnosisResponse
from app.providers.anthropic_provider import DEFAULT_MODEL, AnthropicProvider
from app.providers.base import LLMProvider, ProviderError
from app.providers.mock_provider import MockProvider
from app.services.diagnosis_service import DiagnosisService

logger = logging.getLogger("revguard.ai_service")

app = FastAPI(title="RevGuard AI Service")


def build_provider() -> LLMProvider:
    """Selects the LLM provider from environment configuration.

    AI_PROVIDER=mock (default) uses the deterministic MockProvider — safe
    for any environment, no credentials required. AI_PROVIDER=anthropic
    requires ANTHROPIC_API_KEY to be set; if it is not, this fails fast at
    startup rather than silently falling back, so a misconfigured
    deployment is never mistaken for one using a real model.
    """
    provider_name = os.getenv("AI_PROVIDER", "mock").strip().lower()

    if provider_name == "mock":
        return MockProvider()

    if provider_name == "anthropic":
        api_key = os.getenv("ANTHROPIC_API_KEY", "")
        if not api_key:
            raise RuntimeError(
                "AI_PROVIDER=anthropic requires ANTHROPIC_API_KEY to be set"
            )
        model = os.getenv("ANTHROPIC_MODEL", DEFAULT_MODEL)
        return AnthropicProvider(api_key=api_key, model=model)

    raise RuntimeError(f"unknown AI_PROVIDER {provider_name!r}")


diagnosis_service = DiagnosisService(build_provider())


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/v1/diagnose", response_model=DiagnosisResponse)
async def diagnose(request: DiagnosisRequest) -> DiagnosisResponse:
    started = time.monotonic()
    logger.info(
        "diagnosis request started",
        extra={
            "recovery_case_id": str(request.case_id),
            "provider": diagnosis_service.provider_name,
            "model": diagnosis_service.provider_model,
        },
    )
    try:
        result = await diagnosis_service.diagnose(request)
    except ProviderError as exc:
        latency_ms = (time.monotonic() - started) * 1000
        logger.warning(
            "diagnosis request failed",
            extra={"recovery_case_id": str(request.case_id), "latency_ms": latency_ms},
        )
        # 502: this service reached out to an upstream (the LLM provider)
        # and that upstream failed. Never fabricate a recommendation here.
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    latency_ms = (time.monotonic() - started) * 1000
    logger.info(
        "diagnosis request succeeded",
        extra={
            "recovery_case_id": str(request.case_id),
            "provider": result.provider,
            "model": result.model,
            "prompt_version": result.prompt_version,
            "latency_ms": latency_ms,
            "action": result.recommendation.action.value,
            "confidence": result.recommendation.confidence,
        },
    )
    return result
