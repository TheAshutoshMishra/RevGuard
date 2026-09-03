package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// economicEvaluationReader is the subset of service.EconomicEngine's API
// this handler needs. Defined at the point of use so the handler can be
// exercised with a fake in tests without a real database.
type economicEvaluationReader interface {
	GetLatestEvaluation(ctx context.Context, recoveryCaseID uuid.UUID) (*domain.RecoveryEconomicEvaluation, error)
}

// handleGetEconomicEvaluation is a minimal, read-only verification
// endpoint: GET /v1/recovery-cases/{id}/economic-evaluation. It exposes
// no way to approve, execute, or otherwise act on an evaluation — the
// Economic Engine only evaluates, and this endpoint only reads back what
// it recorded.
func handleGetEconomicEvaluation(reader economicEvaluationReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery case id")
			return
		}

		evaluation, err := reader.GetLatestEvaluation(r.Context(), caseID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				writeError(w, http.StatusNotFound, "no economic evaluation found for this recovery case")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load economic evaluation")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(toEconomicEvaluationResponse(evaluation))
	}
}

type economicEvaluationResponse struct {
	ID                                 string `json:"id"`
	RecoveryCaseID                     string `json:"recovery_case_id"`
	RecoveryDiagnosisID                string `json:"recovery_diagnosis_id"`
	RecommendedAction                  string `json:"recommended_action"`
	Currency                           string `json:"currency"`
	RevenueAtRiskMinorUnits            int64  `json:"revenue_at_risk_minor_units"`
	RecoveryProbabilityBps             int    `json:"recovery_probability_bps"`
	ExpectedGrossRecoveryMinorUnits    int64  `json:"expected_gross_recovery_minor_units"`
	ActionCostMinorUnits               int64  `json:"action_cost_minor_units"`
	RiskCostMinorUnits                 int64  `json:"risk_cost_minor_units"`
	ExpectedIncrementalValueMinorUnits int64  `json:"expected_incremental_value_minor_units"`
	EstimatorName                      string `json:"estimator_name"`
	EstimatorVersion                   string `json:"estimator_version"`
	EconomicModelVersion               string `json:"economic_model_version"`
	CreatedAt                          string `json:"created_at"`
}

func toEconomicEvaluationResponse(e *domain.RecoveryEconomicEvaluation) economicEvaluationResponse {
	return economicEvaluationResponse{
		ID:                                 e.ID.String(),
		RecoveryCaseID:                     e.RecoveryCaseID.String(),
		RecoveryDiagnosisID:                e.RecoveryDiagnosisID.String(),
		RecommendedAction:                  string(e.RecommendedAction),
		Currency:                           string(e.RevenueAtRisk.Currency),
		RevenueAtRiskMinorUnits:            e.RevenueAtRisk.MinorUnits,
		RecoveryProbabilityBps:             int(e.RecoveryProbabilityBps),
		ExpectedGrossRecoveryMinorUnits:    e.ExpectedGrossRecovery.MinorUnits,
		ActionCostMinorUnits:               e.ActionCost.MinorUnits,
		RiskCostMinorUnits:                 e.RiskCost.MinorUnits,
		ExpectedIncrementalValueMinorUnits: e.ExpectedIncrementalValueMinorUnits,
		EstimatorName:                      e.EstimatorName,
		EstimatorVersion:                   e.EstimatorVersion,
		EconomicModelVersion:               e.EconomicModelVersion,
		CreatedAt:                          e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
