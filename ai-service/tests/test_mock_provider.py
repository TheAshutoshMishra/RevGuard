import uuid

import pytest

from app.models.diagnosis import (
    FailureCategory,
    PaymentAttemptContext,
    RecommendedAction,
    RecoveryActionContext,
    RecoveryContext,
)
from app.providers.mock_provider import MockProvider


def base_context(**overrides) -> RecoveryContext:
    kwargs = {
        "recovery_case_id": uuid.uuid4(),
        "merchant_id": uuid.uuid4(),
        "customer_id": uuid.uuid4(),
        "payment_id": uuid.uuid4(),
        "amount_minor_units": 49950,
        "currency": "INR",
        "payment_status": "FAILED",
        "triggering_event_type": "payment.failed",
        "payment_attempts": [],
        "previous_recovery_actions": [],
    }
    kwargs.update(overrides)
    return RecoveryContext(**kwargs)


def test_mock_provider_identifies_itself_as_mock():
    provider = MockProvider()
    assert provider.name == "mock"
    assert "mock" in provider.model


@pytest.mark.asyncio
async def test_insufficient_funds_signal():
    ctx = base_context(
        payment_attempts=[
            PaymentAttemptContext(attempt_number=1, status="FAILED", failure_code="insufficient_funds")
        ]
    )
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.diagnosis.failure_category == FailureCategory.INSUFFICIENT_FUNDS
    assert output.recommendation.action == RecommendedAction.SEND_PAYMENT_LINK


@pytest.mark.asyncio
async def test_authentication_signal():
    ctx = base_context(
        payment_attempts=[
            PaymentAttemptContext(attempt_number=1, status="FAILED", failure_code="auth_failed")
        ]
    )
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.diagnosis.failure_category == FailureCategory.AUTHENTICATION_ISSUE
    assert output.recommendation.action == RecommendedAction.REQUEST_PAYMENT_METHOD_CHANGE


@pytest.mark.asyncio
async def test_mandate_failed_event():
    ctx = base_context(triggering_event_type="mandate.failed")
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.diagnosis.failure_category == FailureCategory.MANDATE_ISSUE
    assert output.recommendation.action == RecommendedAction.ESCALATE_TO_HUMAN


@pytest.mark.asyncio
async def test_checkout_abandoned_event():
    ctx = base_context(triggering_event_type="checkout.abandoned")
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.diagnosis.failure_category == FailureCategory.CUSTOMER_ABANDONMENT
    assert output.recommendation.action == RecommendedAction.SEND_REMINDER


@pytest.mark.asyncio
async def test_many_failed_attempts_escalates():
    ctx = base_context(
        payment_attempts=[
            PaymentAttemptContext(attempt_number=i, status="FAILED") for i in range(1, 4)
        ]
    )
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.recommendation.action == RecommendedAction.ESCALATE_TO_HUMAN
    assert "multiple_failed_attempts" in output.risk_flags


@pytest.mark.asyncio
async def test_repeated_recovery_actions_stops():
    ctx = base_context(
        previous_recovery_actions=[
            RecoveryActionContext(action_type="RETRY_PAYMENT", status="FAILED", attempt_number=1),
            RecoveryActionContext(action_type="SEND_REMINDER", status="FAILED", attempt_number=2),
        ]
    )
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.recommendation.action == RecommendedAction.STOP_RECOVERY


@pytest.mark.asyncio
async def test_default_transient_failure():
    ctx = base_context()
    output = await MockProvider().generate_diagnosis(ctx)
    assert output.diagnosis.failure_category == FailureCategory.TRANSIENT_FAILURE
    assert output.recommendation.action == RecommendedAction.RETRY_PAYMENT


@pytest.mark.asyncio
async def test_high_value_payment_risk_flag():
    ctx = base_context(amount_minor_units=1_000_000)
    output = await MockProvider().generate_diagnosis(ctx)
    assert "high_value_payment" in output.risk_flags


@pytest.mark.asyncio
async def test_confidence_always_in_bounds():
    output = await MockProvider().generate_diagnosis(base_context())
    assert 0.0 <= output.recommendation.confidence <= 1.0
