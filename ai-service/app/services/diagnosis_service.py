"""Orchestrates a single diagnosis request: call the configured
LLMProvider and wrap its output into the full, versioned
DiagnosisResponse. Contains no infrastructure calls, no policy logic, and
makes no authorization decisions — it only produces a recommendation.
"""

from __future__ import annotations

from datetime import datetime, timezone

from app.models.diagnosis import DiagnosisRequest, DiagnosisResponse
from app.prompts.diagnosis_v1 import PROMPT_VERSION
from app.providers.base import LLMProvider


class DiagnosisService:
    def __init__(self, provider: LLMProvider):
        self._provider = provider

    @property
    def provider_name(self) -> str:
        return self._provider.name

    @property
    def provider_model(self) -> str:
        return self._provider.model

    async def diagnose(self, request: DiagnosisRequest) -> DiagnosisResponse:
        output = await self._provider.generate_diagnosis(request.context)
        return DiagnosisResponse(
            case_id=request.case_id,
            diagnosis=output.diagnosis,
            recommendation=output.recommendation,
            risk_flags=output.risk_flags,
            explanation=output.explanation,
            provider=self._provider.name,
            model=self._provider.model,
            prompt_version=PROMPT_VERSION,
            generated_at=datetime.now(timezone.utc),
        )
