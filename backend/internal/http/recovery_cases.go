package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// recoveryCaseListMaxLimit bounds the ?limit= query parameter for the
// same reason /v1/evaluation bounds ?cases= — an unauthenticated,
// read-only endpoint should not accept an arbitrarily expensive request.
const recoveryCaseListMaxLimit = 200

type recoveryCaseSummary struct {
	ID                                 string   `json:"id"`
	MerchantID                         string   `json:"merchant_id"`
	CustomerID                         string   `json:"customer_id"`
	PaymentID                          string   `json:"payment_id"`
	Status                             string   `json:"status"`
	RevenueAtRiskMinorUnits            int64    `json:"revenue_at_risk_minor_units"`
	Currency                           string   `json:"currency"`
	CreatedAt                          string   `json:"created_at"`
	UpdatedAt                          string   `json:"updated_at"`
	FailureCategory                    string   `json:"failure_category,omitempty"`
	RecommendedAction                  string   `json:"recommended_action,omitempty"`
	Confidence                         *float64 `json:"confidence,omitempty"`
	ExpectedIncrementalValueMinorUnits *int64   `json:"expected_incremental_value_minor_units,omitempty"`
	ActionCostMinorUnits               *int64   `json:"action_cost_minor_units,omitempty"`
	RiskCostMinorUnits                 *int64   `json:"risk_cost_minor_units,omitempty"`
	PolicyDecision                     string   `json:"policy_decision,omitempty"`
	AuthorizedAction                   string   `json:"authorized_action,omitempty"`
	ExecutionStatus                    string   `json:"execution_status,omitempty"`
	OutcomeStatus                      string   `json:"outcome_status,omitempty"`
	RecoveredAmountMinorUnits          *int64   `json:"recovered_amount_minor_units,omitempty"`
}

// handleListRecoveryCases is a read-only dashboard endpoint (Milestone
// 11): GET /v1/recovery-cases?status=&limit=&offset=. It never mutates
// state and never selects, authorizes, or executes an action — it only
// reads back what the real pipeline (Milestones 2-7) already decided.
// Note: this composes one query per case for its latest
// diagnosis/decision/action/outcome (N+1), which is fine for demo-scale
// data but not written for high-volume production listing — see
// docs/architecture/dashboard.md's "Known limitations".
func handleListRecoveryCases(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate query parameters before checking pool availability:
		// a malformed request is a client error (400) regardless of
		// whether the database happens to be configured.
		filter := repository.RecoveryCaseListFilter{Limit: 50}
		if v := r.URL.Query().Get("status"); v != "" {
			status := domain.RecoveryCaseStatus(v)
			if !status.Valid() {
				writeError(w, http.StatusBadRequest, "invalid status")
				return
			}
			filter.Status = &status
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				writeError(w, http.StatusBadRequest, "invalid limit")
				return
			}
			filter.Limit = n
		}
		if filter.Limit > recoveryCaseListMaxLimit {
			filter.Limit = recoveryCaseListMaxLimit
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "invalid offset")
				return
			}
			filter.Offset = n
		}

		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "database not configured")
			return
		}

		ctx := r.Context()
		caseRepo := repository.NewPostgresRecoveryCaseRepository(pool)
		diagnosisRepo := repository.NewPostgresRecoveryDiagnosisRepository(pool)
		evaluationRepo := repository.NewPostgresRecoveryEconomicEvaluationRepository(pool)
		decisionRepo := repository.NewPostgresPolicyDecisionRepository(pool)
		actionRepo := repository.NewPostgresRecoveryActionRepository(pool)
		outcomeRepo := repository.NewPostgresRecoveryOutcomeRepository(pool)

		cases, err := caseRepo.List(ctx, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list recovery cases")
			return
		}
		total, err := caseRepo.Count(ctx, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count recovery cases")
			return
		}

		summaries := make([]recoveryCaseSummary, 0, len(cases))
		for _, c := range cases {
			s := recoveryCaseSummary{
				ID: c.ID.String(), MerchantID: c.MerchantID.String(), CustomerID: c.CustomerID.String(),
				PaymentID: c.PaymentID.String(), Status: string(c.Status),
				RevenueAtRiskMinorUnits: c.RevenueAtRisk.MinorUnits, Currency: string(c.RevenueAtRisk.Currency),
				CreatedAt: c.CreatedAt.Format(timeFormat), UpdatedAt: c.UpdatedAt.Format(timeFormat),
			}

			if diagnoses, err := diagnosisRepo.ListByRecoveryCaseID(ctx, c.ID); err == nil && len(diagnoses) > 0 {
				d := diagnoses[0]
				s.FailureCategory = string(d.FailureCategory)
				s.RecommendedAction = string(d.RecommendedAction)
				confidence := d.Confidence
				s.Confidence = &confidence
			}
			if evaluation, err := evaluationRepo.GetLatestByRecoveryCaseID(ctx, c.ID); err == nil {
				v := evaluation.ExpectedIncrementalValueMinorUnits
				s.ExpectedIncrementalValueMinorUnits = &v
				actionCost := evaluation.ActionCost.MinorUnits
				riskCost := evaluation.RiskCost.MinorUnits
				s.ActionCostMinorUnits = &actionCost
				s.RiskCostMinorUnits = &riskCost
			}
			if decision, err := decisionRepo.GetLatestByRecoveryCaseID(ctx, c.ID); err == nil {
				s.PolicyDecision = string(decision.Outcome)
				s.AuthorizedAction = string(decision.AuthorizedAction)
			}
			if actions, err := actionRepo.ListByRecoveryCaseID(ctx, c.ID); err == nil && len(actions) > 0 {
				latestAction := actions[len(actions)-1]
				s.ExecutionStatus = string(latestAction.Status)
				if outcome, err := outcomeRepo.GetByRecoveryActionID(ctx, latestAction.ID); err == nil {
					s.OutcomeStatus = string(outcome.Status)
					amount := outcome.RecoveredAmount.MinorUnits
					s.RecoveredAmountMinorUnits = &amount
				}
			}

			summaries = append(summaries, s)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"cases":  summaries,
			"total":  total,
			"limit":  filter.Limit,
			"offset": filter.Offset,
		})
	}
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

type auditEventResponse struct {
	ID        string          `json:"id"`
	EventType string          `json:"event_type"`
	ActorType string          `json:"actor_type"`
	ActorID   string          `json:"actor_id,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt string          `json:"created_at"`
}

type recoveryActionResponse struct {
	ID                string                   `json:"id"`
	ActionType        string                   `json:"action_type"`
	Status            string                   `json:"status"`
	AttemptNumber     int                      `json:"attempt_number"`
	Provider          string                   `json:"provider"`
	ProviderReference string                   `json:"provider_reference,omitempty"`
	ErrorCode         string                   `json:"error_code,omitempty"`
	RequestedAt       string                   `json:"requested_at"`
	ExecutedAt        string                   `json:"executed_at,omitempty"`
	Outcome           *recoveryOutcomeResponse `json:"outcome,omitempty"`
}

type recoveryOutcomeResponse struct {
	Status                    string `json:"status"`
	RecoveredAmountMinorUnits int64  `json:"recovered_amount_minor_units"`
	Currency                  string `json:"currency"`
	ExternalReference         string `json:"external_reference,omitempty"`
	Provider                  string `json:"provider"`
	Source                    string `json:"source"`
	ObservedAt                string `json:"observed_at"`
}

type recoveryCaseDetailResponse struct {
	Case               recoveryCaseSummary         `json:"case"`
	Diagnoses          []recoveryDiagnosisResponse `json:"diagnoses"`
	EconomicEvaluation *economicEvaluationResponse `json:"economic_evaluation,omitempty"`
	PolicyDecision     *policyDecisionResponse     `json:"policy_decision,omitempty"`
	Actions            []recoveryActionResponse    `json:"actions"`
	AuditTrail         []auditEventResponse        `json:"audit_trail"`
}

type recoveryDiagnosisResponse struct {
	ID                   string   `json:"id"`
	FailureCategory      string   `json:"failure_category"`
	DiagnosisReason      string   `json:"diagnosis_reason"`
	RecommendedAction    string   `json:"recommended_action"`
	RecommendationReason string   `json:"recommendation_reason"`
	Confidence           float64  `json:"confidence"`
	RiskFlags            []string `json:"risk_flags"`
	Explanation          string   `json:"explanation"`
	Provider             string   `json:"provider"`
	Model                string   `json:"model"`
	PromptVersion        string   `json:"prompt_version"`
	GeneratedAt          string   `json:"generated_at"`
}

// handleGetRecoveryCaseDetail is a read-only dashboard endpoint
// (Milestone 11): GET /v1/recovery-cases/{id}. It composes every
// already-persisted record for a case (diagnosis, economic evaluation,
// policy decision, recovery actions + their financial outcomes, and the
// full audit trail) purely by reading — it creates nothing, authorizes
// nothing, and executes nothing. No provider credential, card data, or
// raw provider response is ever included (RecoveryAction/RecoveryOutcome
// already never store those — see docs/architecture/execution-engine.md
// and webhooks-reconciliation.md).
func handleGetRecoveryCaseDetail(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery case id")
			return
		}
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "database not configured")
			return
		}

		ctx := r.Context()
		caseRepo := repository.NewPostgresRecoveryCaseRepository(pool)
		diagnosisRepo := repository.NewPostgresRecoveryDiagnosisRepository(pool)
		evaluationRepo := repository.NewPostgresRecoveryEconomicEvaluationRepository(pool)
		decisionRepo := repository.NewPostgresPolicyDecisionRepository(pool)
		actionRepo := repository.NewPostgresRecoveryActionRepository(pool)
		outcomeRepo := repository.NewPostgresRecoveryOutcomeRepository(pool)
		auditRepo := repository.NewPostgresAuditEventRepository(pool)

		c, err := caseRepo.GetByID(ctx, caseID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				writeError(w, http.StatusNotFound, "recovery case not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load recovery case")
			return
		}

		resp := recoveryCaseDetailResponse{
			Case: recoveryCaseSummary{
				ID: c.ID.String(), MerchantID: c.MerchantID.String(), CustomerID: c.CustomerID.String(),
				PaymentID: c.PaymentID.String(), Status: string(c.Status),
				RevenueAtRiskMinorUnits: c.RevenueAtRisk.MinorUnits, Currency: string(c.RevenueAtRisk.Currency),
				CreatedAt: c.CreatedAt.Format(timeFormat), UpdatedAt: c.UpdatedAt.Format(timeFormat),
			},
		}

		if diagnoses, err := diagnosisRepo.ListByRecoveryCaseID(ctx, caseID); err == nil {
			for _, d := range diagnoses {
				resp.Diagnoses = append(resp.Diagnoses, recoveryDiagnosisResponse{
					ID: d.ID.String(), FailureCategory: string(d.FailureCategory), DiagnosisReason: d.DiagnosisReason,
					RecommendedAction: string(d.RecommendedAction), RecommendationReason: d.RecommendationReason,
					Confidence: d.Confidence, RiskFlags: d.RiskFlags, Explanation: d.Explanation,
					Provider: d.Provider, Model: d.Model, PromptVersion: d.PromptVersion,
					GeneratedAt: d.GeneratedAt.Format(timeFormat),
				})
			}
		}

		if evaluation, err := evaluationRepo.GetLatestByRecoveryCaseID(ctx, caseID); err == nil {
			er := toEconomicEvaluationResponse(evaluation)
			resp.EconomicEvaluation = &er
		}

		if decision, err := decisionRepo.GetLatestByRecoveryCaseID(ctx, caseID); err == nil {
			dr := toPolicyDecisionResponse(decision)
			resp.PolicyDecision = &dr
		}

		if actions, err := actionRepo.ListByRecoveryCaseID(ctx, caseID); err == nil {
			for _, a := range actions {
				ar := recoveryActionResponse{
					ID: a.ID.String(), ActionType: string(a.ActionType), Status: string(a.Status),
					AttemptNumber: a.AttemptNumber, Provider: a.Provider,
					ProviderReference: a.ProviderReference, ErrorCode: a.ErrorCode,
					RequestedAt: a.RequestedAt.Format(timeFormat),
				}
				if a.ExecutedAt != nil {
					ar.ExecutedAt = a.ExecutedAt.Format(timeFormat)
				}
				if outcome, err := outcomeRepo.GetByRecoveryActionID(ctx, a.ID); err == nil {
					ar.Outcome = &recoveryOutcomeResponse{
						Status: string(outcome.Status), RecoveredAmountMinorUnits: outcome.RecoveredAmount.MinorUnits,
						Currency: string(outcome.RecoveredAmount.Currency), ExternalReference: outcome.ExternalReference,
						Provider: outcome.Provider, Source: string(outcome.Source),
						ObservedAt: outcome.ObservedAt.Format(timeFormat),
					}
				}
				resp.Actions = append(resp.Actions, ar)
			}
		}

		if events, err := auditRepo.ListByRecoveryCaseID(ctx, caseID); err == nil {
			for _, e := range events {
				resp.AuditTrail = append(resp.AuditTrail, auditEventResponse{
					ID: e.ID.String(), EventType: e.EventType, ActorType: string(e.ActorType),
					ActorID: e.ActorID, Metadata: json.RawMessage(nonEmptyJSON(e.Metadata)),
					CreatedAt: e.CreatedAt.Format(timeFormat),
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}

func nonEmptyJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}
