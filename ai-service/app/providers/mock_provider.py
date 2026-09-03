"""Deterministic mock LLM provider.

THIS IS A TEST/DEVELOPMENT PROVIDER, NOT REAL AI OUTPUT. It applies fixed,
deterministic rules to the context so automated tests and local
development get stable, reproducible responses without any external
dependency or API key. Its `name` is always "mock" and its `model` is
always "mock-rule-based-v1" — that combination is the explicit signal
(recorded on every stored recommendation) that a given diagnosis did not
come from a real model. Never present mock output as real AI output.
"""

from __future__ import annotations

from app.models.diagnosis import (
    Diagnosis,
    FailureCategory,
    Recommendation,
    RecommendedAction,
    RecoveryContext,
)
from app.providers.base import LLMProvider, ProviderOutput

_HIGH_VALUE_THRESHOLD_MINOR_UNITS = 100_000  # e.g. INR 1,000.00


class MockProvider(LLMProvider):
    @property
    def name(self) -> str:
        return "mock"

    @property
    def model(self) -> str:
        return "mock-rule-based-v1"

    async def generate_diagnosis(self, context: RecoveryContext) -> ProviderOutput:
        failure_codes = [
            (a.failure_code or "").lower() for a in context.payment_attempts
        ]

        if any("insufficient" in code for code in failure_codes):
            category = FailureCategory.INSUFFICIENT_FUNDS
            action = RecommendedAction.SEND_PAYMENT_LINK
            confidence = 0.75
            reason = "A payment attempt failed with an insufficient-funds code."
        elif any("auth" in code for code in failure_codes):
            category = FailureCategory.AUTHENTICATION_ISSUE
            action = RecommendedAction.REQUEST_PAYMENT_METHOD_CHANGE
            confidence = 0.70
            reason = "A payment attempt failed with an authentication-related code."
        elif context.triggering_event_type == "mandate.failed":
            category = FailureCategory.MANDATE_ISSUE
            action = RecommendedAction.ESCALATE_TO_HUMAN
            confidence = 0.60
            reason = "The triggering event was a mandate failure."
        elif context.triggering_event_type == "checkout.abandoned":
            category = FailureCategory.CUSTOMER_ABANDONMENT
            action = RecommendedAction.SEND_REMINDER
            confidence = 0.55
            reason = "The triggering event was an abandoned checkout."
        elif len(context.payment_attempts) >= 3:
            category = FailureCategory.UNKNOWN
            action = RecommendedAction.ESCALATE_TO_HUMAN
            confidence = 0.50
            reason = "Three or more payment attempts have already failed."
        elif len(context.previous_recovery_actions) >= 2:
            category = FailureCategory.UNKNOWN
            action = RecommendedAction.STOP_RECOVERY
            confidence = 0.50
            reason = "Two or more recovery actions have already been attempted for this case."
        else:
            category = FailureCategory.TRANSIENT_FAILURE
            action = RecommendedAction.RETRY_PAYMENT
            confidence = 0.65
            reason = "No specific failure signal found; treating as a transient failure."

        risk_flags: list[str] = []
        if context.amount_minor_units >= _HIGH_VALUE_THRESHOLD_MINOR_UNITS:
            risk_flags.append("high_value_payment")
        if len(context.payment_attempts) >= 3:
            risk_flags.append("multiple_failed_attempts")

        return ProviderOutput(
            diagnosis=Diagnosis(
                reason=reason,
                failure_category=category,
                customer_context=(
                    f"{len(context.payment_attempts)} payment attempt(s), "
                    f"{len(context.previous_recovery_actions)} prior recovery action(s)."
                ),
                recommended_strategy=action.value,
            ),
            recommendation=Recommendation(
                action=action,
                reason=reason,
                confidence=confidence,
            ),
            risk_flags=risk_flags,
            explanation=(
                f"[mock provider] {reason} Recommending {action.value} "
                f"with confidence {confidence:.2f}."
            ),
        )
