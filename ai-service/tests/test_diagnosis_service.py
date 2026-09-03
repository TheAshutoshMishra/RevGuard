import uuid

import pytest

from app.models.diagnosis import (
    Diagnosis,
    DiagnosisRequest,
    FailureCategory,
    Recommendation,
    RecommendedAction,
    RecoveryContext,
)
from app.providers.base import ProviderError, ProviderOutput
from app.services.diagnosis_service import DiagnosisService


class StubProvider:
    def __init__(self, output=None, error=None):
        self._output = output
        self._error = error

    @property
    def name(self) -> str:
        return "stub"

    @property
    def model(self) -> str:
        return "stub-v1"

    async def generate_diagnosis(self, context):
        if self._error:
            raise self._error
        return self._output


def a_request() -> DiagnosisRequest:
    return DiagnosisRequest(
        case_id=uuid.uuid4(),
        context=RecoveryContext(
            recovery_case_id=uuid.uuid4(),
            merchant_id=uuid.uuid4(),
            customer_id=uuid.uuid4(),
            payment_id=uuid.uuid4(),
            amount_minor_units=1000,
            currency="INR",
            payment_status="FAILED",
            triggering_event_type="payment.failed",
        ),
    )


@pytest.mark.asyncio
async def test_diagnose_wraps_provider_output_with_versioning_metadata():
    output = ProviderOutput(
        diagnosis=Diagnosis(
            reason="r", failure_category=FailureCategory.TRANSIENT_FAILURE,
            customer_context="c", recommended_strategy="retry_payment",
        ),
        recommendation=Recommendation(action=RecommendedAction.RETRY_PAYMENT, reason="r", confidence=0.5),
        risk_flags=[],
        explanation="e",
    )
    service = DiagnosisService(StubProvider(output=output))
    request = a_request()

    result = await service.diagnose(request)

    assert result.case_id == request.case_id
    assert result.provider == "stub"
    assert result.model == "stub-v1"
    assert result.prompt_version == "v1"
    assert result.generated_at is not None


@pytest.mark.asyncio
async def test_diagnose_propagates_provider_error():
    service = DiagnosisService(StubProvider(error=ProviderError("boom")))
    with pytest.raises(ProviderError):
        await service.diagnose(a_request())
