package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecoveryEconomicEvaluation is the durable, auditable record of a single
// deterministic economic evaluation of a RecoveryDiagnosis's
// recommendation (Milestone 4).
//
// It answers whether the recommended action has positive expected
// economic value. It does NOT decide whether the action is allowed —
// that is a policy decision belonging to a later milestone. Persisting
// this record never changes RecoveryCase.Status; the case remains
// ANALYZED after evaluation.
//
// Tied to the exact RecoveryDiagnosis that produced the recommendation
// (RecoveryDiagnosisID), and unique per diagnosis (see migration 000013)
// so re-running evaluation for the same diagnosis is a safe no-op. A new
// diagnosis (e.g. from re-analysis) may produce a new, separate
// evaluation — rows here are never overwritten.
type RecoveryEconomicEvaluation struct {
	ID                  uuid.UUID
	RecoveryCaseID      uuid.UUID
	RecoveryDiagnosisID uuid.UUID
	RecommendedAction   RecommendedAction

	// RevenueAtRisk, ExpectedGrossRecovery, ActionCost, and RiskCost are
	// all non-negative by construction (see the economic formulas in
	// backend/internal/service/economic_calculations.go) and therefore
	// modeled as Money.
	RevenueAtRisk          Money
	RecoveryProbabilityBps ProbabilityBasisPoints
	ExpectedGrossRecovery  Money
	ActionCost             Money
	RiskCost               Money

	// ExpectedIncrementalValueMinorUnits is signed (gross recovery minus
	// costs can be negative) and therefore deliberately NOT a Money value
	// (domain.Money rejects negative amounts). It shares its currency
	// with RevenueAtRisk.Currency.
	ExpectedIncrementalValueMinorUnits int64

	// EstimatorName/EstimatorVersion identify the RecoveryProbabilityEstimator
	// implementation that produced RecoveryProbabilityBps.
	// EconomicModelVersion identifies the action-economics table and
	// formula version used. All three are recorded so a stored evaluation
	// stays reproducible and attributable, matching the pattern
	// RecoveryDiagnosis already established for provider/model/prompt_version.
	EstimatorName        string
	EstimatorVersion     string
	EconomicModelVersion string

	CreatedAt time.Time
}
