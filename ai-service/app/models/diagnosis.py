"""Structured request/response models for the recovery diagnosis endpoint.

These are the only shapes allowed to cross the Python -> Go boundary. No
arbitrary/unvalidated JSON is returned from /v1/diagnose: every field is a
typed, constrained Pydantic field, and the controlled vocabularies
(FailureCategory, RecommendedAction) are enums, not free text.
"""

from __future__ import annotations

from datetime import datetime
from enum import Enum
from uuid import UUID

from pydantic import BaseModel, Field


class FailureCategory(str, Enum):
    """Controlled vocabulary for why a payment/recovery is at risk.

    Intentionally small. Do not add categories without updating this
    enum, the system prompt, and the mirrored Go enum
    (backend/internal/domain/recovery_diagnosis.go).
    """

    TRANSIENT_FAILURE = "transient_failure"
    INSUFFICIENT_FUNDS = "insufficient_funds"
    PAYMENT_METHOD_ISSUE = "payment_method_issue"
    AUTHENTICATION_ISSUE = "authentication_issue"
    MANDATE_ISSUE = "mandate_issue"
    CUSTOMER_ABANDONMENT = "customer_abandonment"
    UNKNOWN = "unknown"


class RecommendedAction(str, Enum):
    """Controlled vocabulary for what the AI recommends.

    This is a RECOMMENDATION only. Neither this service nor the action
    name itself authorizes anything — Go's policy engine (a later
    milestone) decides whether and how to act on it, and Go's execution
    layer (also later) is the only thing that ever calls out to payment
    infrastructure. The identifiers mirror
    backend/internal/domain.RecoveryActionType (kept as a distinct Go type
    on purpose — see that file for why).
    """

    RETRY_PAYMENT = "retry_payment"
    SEND_PAYMENT_LINK = "send_payment_link"
    REQUEST_PAYMENT_METHOD_CHANGE = "request_payment_method_change"
    SEND_REMINDER = "send_reminder"
    ESCALATE_TO_HUMAN = "escalate_to_human"
    STOP_RECOVERY = "stop_recovery"


class PaymentAttemptContext(BaseModel):
    attempt_number: int = Field(ge=1)
    status: str
    failure_code: str | None = None
    failure_reason: str | None = None


class RecoveryActionContext(BaseModel):
    action_type: str
    status: str
    attempt_number: int = Field(ge=1)


class RecoveryContext(BaseModel):
    """The minimal context Go assembles for diagnosis. Never contains
    card numbers, CVV, authentication credentials, API keys, or any other
    raw payment secret — see RecoveryContextBuilder on the Go side.
    """

    recovery_case_id: UUID
    merchant_id: UUID
    customer_id: UUID
    payment_id: UUID
    amount_minor_units: int = Field(ge=0)
    currency: str = Field(min_length=3, max_length=3)
    payment_status: str
    triggering_event_type: str
    payment_attempts: list[PaymentAttemptContext] = Field(default_factory=list)
    previous_recovery_actions: list[RecoveryActionContext] = Field(default_factory=list)


class DiagnosisRequest(BaseModel):
    case_id: UUID
    context: RecoveryContext


class Diagnosis(BaseModel):
    reason: str = Field(min_length=1)
    failure_category: FailureCategory
    customer_context: str = Field(min_length=1)
    recommended_strategy: str = Field(min_length=1)


class Recommendation(BaseModel):
    action: RecommendedAction
    reason: str = Field(min_length=1)
    confidence: float = Field(ge=0.0, le=1.0)


class DiagnosisResponse(BaseModel):
    case_id: UUID
    diagnosis: Diagnosis
    recommendation: Recommendation
    risk_flags: list[str] = Field(default_factory=list)
    explanation: str = Field(min_length=1)

    # Versioning metadata — recorded so a stored recommendation is
    # reproducible: which provider, which model, which prompt version,
    # and when.
    provider: str
    model: str
    prompt_version: str
    generated_at: datetime
