import uuid

import pytest
from pydantic import ValidationError

from app.models.diagnosis import (
    Diagnosis,
    DiagnosisRequest,
    FailureCategory,
    Recommendation,
    RecommendedAction,
    RecoveryContext,
)


def valid_context_kwargs() -> dict:
    return {
        "recovery_case_id": str(uuid.uuid4()),
        "merchant_id": str(uuid.uuid4()),
        "customer_id": str(uuid.uuid4()),
        "payment_id": str(uuid.uuid4()),
        "amount_minor_units": 49950,
        "currency": "INR",
        "payment_status": "FAILED",
        "triggering_event_type": "payment.failed",
        "payment_attempts": [],
        "previous_recovery_actions": [],
    }


def test_valid_request_parses():
    req = DiagnosisRequest(case_id=str(uuid.uuid4()), context=valid_context_kwargs())
    assert req.context.currency == "INR"


def test_missing_case_id_rejected():
    with pytest.raises(ValidationError):
        DiagnosisRequest(context=valid_context_kwargs())


def test_malformed_context_missing_field_rejected():
    ctx = valid_context_kwargs()
    del ctx["payment_id"]
    with pytest.raises(ValidationError):
        DiagnosisRequest(case_id=str(uuid.uuid4()), context=ctx)


def test_invalid_currency_length_rejected():
    ctx = valid_context_kwargs()
    ctx["currency"] = "INRX"
    with pytest.raises(ValidationError):
        DiagnosisRequest(case_id=str(uuid.uuid4()), context=ctx)


def test_negative_amount_rejected():
    ctx = valid_context_kwargs()
    ctx["amount_minor_units"] = -1
    with pytest.raises(ValidationError):
        DiagnosisRequest(case_id=str(uuid.uuid4()), context=ctx)


def test_invalid_aggregate_id_rejected():
    ctx = valid_context_kwargs()
    ctx["payment_id"] = "not-a-uuid"
    with pytest.raises(ValidationError):
        DiagnosisRequest(case_id=str(uuid.uuid4()), context=ctx)


def _valid_diagnosis_kwargs() -> dict:
    return {
        "reason": "test",
        "failure_category": FailureCategory.TRANSIENT_FAILURE,
        "customer_context": "test",
        "recommended_strategy": "retry_payment",
    }


def test_valid_recommendation():
    rec = Recommendation(action=RecommendedAction.RETRY_PAYMENT, reason="x", confidence=0.82)
    assert rec.confidence == 0.82


def test_confidence_below_zero_rejected():
    with pytest.raises(ValidationError):
        Recommendation(action=RecommendedAction.RETRY_PAYMENT, reason="x", confidence=-0.01)


def test_confidence_above_one_rejected():
    with pytest.raises(ValidationError):
        Recommendation(action=RecommendedAction.RETRY_PAYMENT, reason="x", confidence=1.01)


def test_unknown_action_rejected():
    with pytest.raises(ValidationError):
        Recommendation(action="teleport_the_payment", reason="x", confidence=0.5)


def test_unknown_failure_category_rejected():
    with pytest.raises(ValidationError):
        Diagnosis(
            reason="x",
            failure_category="alien_abduction",
            customer_context="x",
            recommended_strategy="x",
        )


def test_valid_diagnosis():
    d = Diagnosis(**_valid_diagnosis_kwargs())
    assert d.failure_category == FailureCategory.TRANSIENT_FAILURE
