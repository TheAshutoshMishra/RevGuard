"""Real LLM provider using the Anthropic Messages API.

Wired via httpx (already a light dependency, no heavy vendor SDK) rather
than the official Anthropic Python SDK, to keep the dependency footprint
minimal per the project's technology discipline. Requires ANTHROPIC_API_KEY
to be set — see .env.example. Never log the API key or the raw request/
response body (see DiagnosisService / main.py for what does get logged).
"""

from __future__ import annotations

import json

import httpx
from pydantic import ValidationError

from app.models.diagnosis import RecoveryContext
from app.prompts.diagnosis_v1 import SYSTEM_PROMPT
from app.providers.base import LLMProvider, ProviderError, ProviderOutput

_ANTHROPIC_MESSAGES_URL = "https://api.anthropic.com/v1/messages"
_ANTHROPIC_VERSION = "2023-06-01"

DEFAULT_MODEL = "claude-sonnet-5"


class AnthropicProvider(LLMProvider):
    def __init__(
        self,
        api_key: str,
        model: str = DEFAULT_MODEL,
        timeout_seconds: float = 20.0,
        client: httpx.AsyncClient | None = None,
    ):
        if not api_key:
            raise ValueError("AnthropicProvider requires a non-empty api_key")
        self._api_key = api_key
        self._model = model
        # `client` is an injection point for tests (a client wired to
        # httpx.MockTransport instead of the network). Production code
        # never passes it.
        self._client = client or httpx.AsyncClient(timeout=timeout_seconds)

    @property
    def name(self) -> str:
        return "anthropic"

    @property
    def model(self) -> str:
        return self._model

    async def generate_diagnosis(self, context: RecoveryContext) -> ProviderOutput:
        try:
            response = await self._client.post(
                _ANTHROPIC_MESSAGES_URL,
                headers={
                    "x-api-key": self._api_key,
                    "anthropic-version": _ANTHROPIC_VERSION,
                    "content-type": "application/json",
                },
                json={
                    "model": self._model,
                    "max_tokens": 1024,
                    "system": SYSTEM_PROMPT,
                    "messages": [
                        {
                            "role": "user",
                            "content": context.model_dump_json(),
                        }
                    ],
                },
            )
        except httpx.TimeoutException as exc:
            raise ProviderError("anthropic provider request timed out") from exc
        except httpx.TransportError as exc:
            raise ProviderError(f"anthropic provider transport error: {exc}") from exc

        if response.status_code >= 400:
            # Never include the raw response body in the error: it may
            # echo back request content, and provider error bodies are
            # not something we want propagating toward end users.
            raise ProviderError(f"anthropic provider returned HTTP {response.status_code}")

        try:
            data = response.json()
            text = data["content"][0]["text"]
            parsed = json.loads(text)
        except (KeyError, IndexError, TypeError, json.JSONDecodeError) as exc:
            raise ProviderError("anthropic provider returned an unparseable response") from exc

        try:
            return ProviderOutput.model_validate(parsed)
        except ValidationError as exc:
            raise ProviderError(f"anthropic provider response failed validation: {exc}") from exc
