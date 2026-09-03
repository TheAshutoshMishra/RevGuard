import json
import uuid

import httpx
import pytest

from app.models.diagnosis import RecoveryContext
from app.providers.anthropic_provider import AnthropicProvider
from app.providers.base import ProviderError


def a_context() -> RecoveryContext:
    return RecoveryContext(
        recovery_case_id=uuid.uuid4(),
        merchant_id=uuid.uuid4(),
        customer_id=uuid.uuid4(),
        payment_id=uuid.uuid4(),
        amount_minor_units=49950,
        currency="INR",
        payment_status="FAILED",
        triggering_event_type="payment.failed",
    )


def _valid_output_json() -> str:
    return json.dumps(
        {
            "diagnosis": {
                "reason": "test reason",
                "failure_category": "transient_failure",
                "customer_context": "test context",
                "recommended_strategy": "retry_payment",
            },
            "recommendation": {
                "action": "retry_payment",
                "reason": "test reason",
                "confidence": 0.8,
            },
            "risk_flags": [],
            "explanation": "test explanation",
        }
    )


def provider_with_transport(handler) -> AnthropicProvider:
    client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    return AnthropicProvider(api_key="test-key", client=client)


@pytest.mark.asyncio
async def test_successful_call_returns_provider_output():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={"content": [{"type": "text", "text": _valid_output_json()}]},
        )

    provider = provider_with_transport(handler)
    output = await provider.generate_diagnosis(a_context())
    assert output.recommendation.action.value == "retry_payment"
    assert output.recommendation.confidence == 0.8


@pytest.mark.asyncio
async def test_timeout_raises_provider_error():
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.TimeoutException("simulated timeout", request=request)

    provider = provider_with_transport(handler)
    with pytest.raises(ProviderError):
        await provider.generate_diagnosis(a_context())


@pytest.mark.asyncio
async def test_transport_error_raises_provider_error():
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("simulated connection failure", request=request)

    provider = provider_with_transport(handler)
    with pytest.raises(ProviderError):
        await provider.generate_diagnosis(a_context())


@pytest.mark.asyncio
async def test_http_500_raises_provider_error():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, text="internal server error")

    provider = provider_with_transport(handler)
    with pytest.raises(ProviderError):
        await provider.generate_diagnosis(a_context())


@pytest.mark.asyncio
async def test_malformed_json_text_raises_provider_error():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={"content": [{"type": "text", "text": "{not valid json"}]},
        )

    provider = provider_with_transport(handler)
    with pytest.raises(ProviderError):
        await provider.generate_diagnosis(a_context())


@pytest.mark.asyncio
async def test_unexpected_response_shape_raises_provider_error():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"unexpected": "shape"})

    provider = provider_with_transport(handler)
    with pytest.raises(ProviderError):
        await provider.generate_diagnosis(a_context())


@pytest.mark.asyncio
async def test_schema_valid_json_but_invalid_recommendation_raises_provider_error():
    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(_valid_output_json())
        body["recommendation"]["confidence"] = 5.0  # out of bounds
        return httpx.Response(
            200,
            json={"content": [{"type": "text", "text": json.dumps(body)}]},
        )

    provider = provider_with_transport(handler)
    with pytest.raises(ProviderError):
        await provider.generate_diagnosis(a_context())


def test_empty_api_key_rejected():
    with pytest.raises(ValueError):
        AnthropicProvider(api_key="")
