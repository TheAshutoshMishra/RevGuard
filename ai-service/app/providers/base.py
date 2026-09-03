"""LLM provider abstraction.

The rest of the service (DiagnosisService, the /v1/diagnose route) is
written against LLMProvider only, never against a concrete implementation.
Swapping providers — mock to real, or one real vendor to another — means
writing one new class here and changing provider selection in main.py; it
never touches request/response validation or the HTTP route.
"""

from __future__ import annotations

from abc import ABC, abstractmethod

from pydantic import BaseModel

from app.models.diagnosis import Diagnosis, Recommendation, RecoveryContext


class ProviderOutput(BaseModel):
    """What a provider must produce. DiagnosisService wraps this with
    provider/model/prompt_version/generated_at to build the full
    DiagnosisResponse — providers don't know about that envelope.
    """

    diagnosis: Diagnosis
    recommendation: Recommendation
    risk_flags: list[str] = []
    explanation: str


class ProviderError(Exception):
    """Raised for any provider-side failure: transport error, timeout,
    non-2xx response, or a response that fails to parse/validate as
    ProviderOutput. The service layer maps this to an HTTP 502 — a
    diagnosis/analysis failure, never mistaken for a payment or recovery
    failure.
    """


class LLMProvider(ABC):
    @property
    @abstractmethod
    def name(self) -> str:
        """Short provider identifier, e.g. "mock" or "anthropic". Recorded
        on every stored recommendation."""

    @property
    @abstractmethod
    def model(self) -> str:
        """Model identifier, e.g. "mock-rule-based-v1" or
        "claude-sonnet-5". Recorded on every stored recommendation."""

    @abstractmethod
    async def generate_diagnosis(self, context: RecoveryContext) -> ProviderOutput:
        """Produce a diagnosis for the given context. Raises ProviderError
        on any failure — never returns a partially-valid or fabricated
        result."""
