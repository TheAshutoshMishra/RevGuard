import uuid

from fastapi.testclient import TestClient

import app.main as main_module
from app.providers.base import ProviderError
from app.services.diagnosis_service import DiagnosisService
from tests.test_diagnosis_service import StubProvider, a_request


def test_health():
    client = TestClient(main_module.app)
    resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def test_diagnose_success(monkeypatch):
    from app.models.diagnosis import (
        Diagnosis,
        FailureCategory,
        Recommendation,
        RecommendedAction,
    )
    from app.providers.base import ProviderOutput

    output = ProviderOutput(
        diagnosis=Diagnosis(
            reason="r", failure_category=FailureCategory.TRANSIENT_FAILURE,
            customer_context="c", recommended_strategy="retry_payment",
        ),
        recommendation=Recommendation(action=RecommendedAction.RETRY_PAYMENT, reason="r", confidence=0.5),
        risk_flags=[],
        explanation="e",
    )
    monkeypatch.setattr(main_module, "diagnosis_service", DiagnosisService(StubProvider(output=output)))

    client = TestClient(main_module.app)
    request = a_request()
    resp = client.post("/v1/diagnose", json=request.model_dump(mode="json"))

    assert resp.status_code == 200
    body = resp.json()
    assert body["recommendation"]["action"] == "retry_payment"
    assert body["provider"] == "stub"


def test_diagnose_missing_case_id_returns_422():
    client = TestClient(main_module.app)
    resp = client.post("/v1/diagnose", json={"context": {}})
    assert resp.status_code == 422


def test_diagnose_provider_error_returns_502(monkeypatch):
    monkeypatch.setattr(
        main_module, "diagnosis_service", DiagnosisService(StubProvider(error=ProviderError("boom")))
    )

    client = TestClient(main_module.app)
    request = a_request()
    resp = client.post("/v1/diagnose", json=request.model_dump(mode="json"))

    assert resp.status_code == 502
