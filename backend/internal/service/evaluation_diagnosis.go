package service

import "revguard/backend/internal/domain"

// evaluationDiagnosis is the deterministic AI-diagnosis stand-in used
// only by the RevGuard strategy in the Milestone 8 evaluation harness.
//
// It deliberately mirrors ai-service/app/providers/mock_provider.go's
// rule priority and confidence values (the project's own deterministic,
// rule-based "not real AI" provider used throughout the Go test suite),
// re-expressed against SyntheticOpportunity's fields instead of raw
// payment-attempt failure codes and triggering event types. This keeps
// the evaluation:
//
//   - deterministic and reproducible (no network call, no real LLM),
//   - honest: it is never presented as "real AI output" — see
//     docs/architecture/evaluation-engine.md — and
//   - consistent with the actual mock behavior the rest of the codebase
//     already relies on, rather than inventing a third, unrelated rule
//     set.
//
// This is NOT a call to the AI service and NOT a new AI implementation:
// it is a fixed function used solely to feed a recommendation into the
// real, unmodified downstream pipeline (HeuristicProbabilityEstimator,
// GetActionEconomics, evaluatePolicyRules).
type evaluationDiagnosis struct {
	FailureCategory   domain.FailureCategory
	RecommendedAction domain.RecommendedAction
	Confidence        float64
}

func deterministicDiagnosis(opp SyntheticOpportunity) evaluationDiagnosis {
	switch {
	case opp.FailureCategory == domain.FailureCategoryInsufficientFunds:
		return evaluationDiagnosis{domain.FailureCategoryInsufficientFunds, domain.RecommendedActionSendPaymentLink, 0.75}
	case opp.FailureCategory == domain.FailureCategoryAuthenticationIssue:
		return evaluationDiagnosis{domain.FailureCategoryAuthenticationIssue, domain.RecommendedActionRequestPaymentMethodChange, 0.70}
	case opp.FailureCategory == domain.FailureCategoryMandateIssue:
		return evaluationDiagnosis{domain.FailureCategoryMandateIssue, domain.RecommendedActionEscalateToHuman, 0.60}
	case opp.FailureCategory == domain.FailureCategoryCustomerAbandonment:
		return evaluationDiagnosis{domain.FailureCategoryCustomerAbandonment, domain.RecommendedActionSendReminder, 0.55}
	case opp.PreviousAttempts >= 3:
		return evaluationDiagnosis{domain.FailureCategoryUnknown, domain.RecommendedActionEscalateToHuman, 0.50}
	case opp.PreviousRecoveryActions >= 2:
		return evaluationDiagnosis{domain.FailureCategoryUnknown, domain.RecommendedActionStopRecovery, 0.50}
	default:
		return evaluationDiagnosis{domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.65}
	}
}
