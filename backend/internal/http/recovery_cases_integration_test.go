package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// testPool requires TEST_DATABASE_URL to point at a disposable
// PostgreSQL database with migrations already applied — the same
// convention internal/service and internal/repository's own test files
// use. Tests are skipped when it is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping dashboard read-endpoint integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedFullRecoveryCase builds one complete, realistic recovery case
// (merchant, customer, payment, case, diagnosis, economic evaluation,
// policy decision, recovery action, recovery outcome, audit event)
// directly through the repository layer, so the dashboard's read
// endpoints can be tested against real, non-fabricated persisted state.
func seedFullRecoveryCase(t *testing.T, pool *pgxpool.Pool) *domain.RecoveryCase {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	merchant := &domain.Merchant{ID: uuid.New(), Name: "Test Merchant", CreatedAt: now, UpdatedAt: now}
	if err := repository.NewPostgresMerchantRepository(pool).Create(ctx, merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	customer := &domain.Customer{
		ID: uuid.New(), MerchantID: merchant.ID, ExternalCustomerID: "cust_" + uuid.New().String()[:8],
		Email: "test@example.com", CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresCustomerRepository(pool).Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	payment := &domain.Payment{
		ID: uuid.New(), MerchantID: merchant.ID, CustomerID: customer.ID,
		ExternalPaymentID: "pay_" + uuid.New().String()[:8], Status: domain.PaymentStatusFailed,
		Amount: domain.Money{MinorUnits: 49950, Currency: "INR"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresPaymentRepository(pool).Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: merchant.ID, CustomerID: customer.ID, PaymentID: payment.ID,
		Status: domain.RecoveryCaseStatusVerifying, RevenueAtRisk: payment.Amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	diagnosis := &domain.RecoveryDiagnosis{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		FailureCategory: domain.FailureCategoryInsufficientFunds, DiagnosisReason: "test",
		CustomerContext: "test", RecommendedStrategy: "retry_payment",
		RecommendedAction: domain.RecommendedActionRetryPayment, RecommendationReason: "test",
		Confidence: 0.85, RiskFlags: []string{}, Explanation: "test",
		Provider: "mock", Model: "mock-rule-based-v1", PromptVersion: "v1", GeneratedAt: now, CreatedAt: now,
	}
	if err := repository.NewPostgresRecoveryDiagnosisRepository(pool).Create(ctx, diagnosis); err != nil {
		t.Fatalf("create diagnosis: %v", err)
	}

	evaluation := &domain.RecoveryEconomicEvaluation{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, RecoveryDiagnosisID: diagnosis.ID,
		RecommendedAction:                  domain.RecommendedActionRetryPayment,
		RevenueAtRisk:                      payment.Amount,
		RecoveryProbabilityBps:             6000,
		ExpectedGrossRecovery:              domain.Money{MinorUnits: 29970, Currency: "INR"},
		ActionCost:                         domain.Money{MinorUnits: 500, Currency: "INR"},
		RiskCost:                           domain.Money{MinorUnits: 250, Currency: "INR"},
		ExpectedIncrementalValueMinorUnits: 29220,
		EstimatorName:                      "heuristic", EstimatorVersion: "heuristic-v1", EconomicModelVersion: "economic-model-v1",
		CreatedAt: now,
	}
	if _, err := repository.NewPostgresRecoveryEconomicEvaluationRepository(pool).TryCreate(ctx, evaluation); err != nil {
		t.Fatalf("create evaluation: %v", err)
	}

	decision := &domain.PolicyDecision{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, RecoveryDiagnosisID: diagnosis.ID,
		RecoveryEconomicEvaluationID: evaluation.ID, Outcome: domain.PolicyDecisionOutcomeAllow,
		AuthorizedAction: domain.RecommendedActionRetryPayment, PolicyVersion: "policy-v1",
		ReasonCodes: []domain.PolicyReasonCode{domain.PolicyReasonPolicyAllowed}, Explanation: "test",
		EvaluatedAt: now, CreatedAt: now,
	}
	if _, err := repository.NewPostgresPolicyDecisionRepository(pool).TryCreate(ctx, decision); err != nil {
		t.Fatalf("create decision: %v", err)
	}

	action := &domain.RecoveryAction{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, ActionType: domain.RecoveryActionTypeRetryPayment,
		Status: domain.RecoveryActionStatusSucceeded, AttemptNumber: 1,
		IdempotencyKey: "policy-decision:" + decision.ID.String(), RequestedAt: now, CreatedAt: now,
		Provider: "fake", ProviderReference: "fake_ref_" + uuid.New().String(),
	}
	if _, err := repository.NewPostgresRecoveryActionRepository(pool).TryCreate(ctx, action); err != nil {
		t.Fatalf("create action: %v", err)
	}

	outcome := &domain.RecoveryOutcome{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, RecoveryActionID: action.ID,
		Status: domain.RecoveryOutcomeStatusSuccess, RecoveredAmount: payment.Amount,
		ExternalReference: "pay_link_" + uuid.New().String(), ObservedAt: now, CreatedAt: now,
		Provider: "fake", Source: domain.RecoveryOutcomeSourceWebhook,
	}
	if _, err := repository.NewPostgresRecoveryOutcomeRepository(pool).TryCreate(ctx, outcome); err != nil {
		t.Fatalf("create outcome: %v", err)
	}

	if err := repository.NewPostgresAuditEventRepository(pool).Create(ctx, &domain.AuditEvent{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, EventType: "recovery_case.created",
		ActorType: domain.AuditActorTypeSystem, Metadata: []byte("{}"), CreatedAt: now,
	}); err != nil {
		t.Fatalf("create audit event: %v", err)
	}

	return recoveryCase
}

func routerWithPool(pool *pgxpool.Pool) http.Handler {
	return NewRouter(nil, nil, nil, nil, nil, nil, pool, "")
}

func TestListRecoveryCases_ReturnsSeededCase(t *testing.T) {
	pool := testPool(t)
	seeded := seedFullRecoveryCase(t, pool)

	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases?limit=200", nil)
	rec := httptest.NewRecorder()
	routerWithPool(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Cases []recoveryCaseSummary `json:"cases"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body.Total < 1 {
		t.Fatal("expected at least 1 case in total count")
	}
	found := false
	for _, c := range body.Cases {
		if c.ID == seeded.ID.String() {
			found = true
			if c.FailureCategory != "insufficient_funds" {
				t.Fatalf("expected failure category insufficient_funds, got %q", c.FailureCategory)
			}
			if c.PolicyDecision != "ALLOW" {
				t.Fatalf("expected policy decision ALLOW, got %q", c.PolicyDecision)
			}
			if c.RecoveredAmountMinorUnits == nil || *c.RecoveredAmountMinorUnits != 49950 {
				t.Fatalf("expected recovered amount 49950, got %v", c.RecoveredAmountMinorUnits)
			}
		}
	}
	if !found {
		t.Fatal("seeded case not found in list response")
	}
}

func TestListRecoveryCases_StatusFilter(t *testing.T) {
	pool := testPool(t)
	seedFullRecoveryCase(t, pool)

	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases?status=VERIFYING&limit=200", nil)
	rec := httptest.NewRecorder()
	routerWithPool(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Cases []recoveryCaseSummary `json:"cases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	for _, c := range body.Cases {
		if c.Status != "VERIFYING" {
			t.Fatalf("status filter leaked a non-VERIFYING case: %+v", c)
		}
	}
}

func TestGetRecoveryCaseDetail_FullComposition(t *testing.T) {
	pool := testPool(t)
	seeded := seedFullRecoveryCase(t, pool)

	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases/"+seeded.ID.String(), nil)
	rec := httptest.NewRecorder()
	routerWithPool(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var detail recoveryCaseDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if detail.Case.ID != seeded.ID.String() {
		t.Fatalf("expected case id %s, got %s", seeded.ID, detail.Case.ID)
	}
	if len(detail.Diagnoses) != 1 || detail.Diagnoses[0].RecommendedAction != "retry_payment" {
		t.Fatalf("expected 1 diagnosis with retry_payment, got %+v", detail.Diagnoses)
	}
	if detail.EconomicEvaluation == nil || detail.EconomicEvaluation.ExpectedIncrementalValueMinorUnits != 29220 {
		t.Fatalf("expected economic evaluation with incremental value 29220, got %+v", detail.EconomicEvaluation)
	}
	if detail.PolicyDecision == nil || detail.PolicyDecision.Decision != "ALLOW" {
		t.Fatalf("expected policy decision ALLOW, got %+v", detail.PolicyDecision)
	}
	if len(detail.Actions) != 1 || detail.Actions[0].Outcome == nil || detail.Actions[0].Outcome.Status != "SUCCESS" {
		t.Fatalf("expected 1 action with a SUCCESS outcome, got %+v", detail.Actions)
	}
	if len(detail.AuditTrail) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(detail.AuditTrail))
	}

	// No secret or raw provider response should ever appear in the
	// composed detail response.
	forbidden := []string{"card_number", "cvv", "api_key", "secret", "password", "key_secret"}
	for _, f := range forbidden {
		if containsFold(rec.Body.String(), f) {
			t.Fatalf("detail response unexpectedly contains forbidden substring %q", f)
		}
	}
}

func TestGetRecoveryCaseDetail_NotFound(t *testing.T) {
	pool := testPool(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	routerWithPool(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetRecoveryCaseDetail_InvalidIDRejected(t *testing.T) {
	pool := testPool(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/recovery-cases/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	routerWithPool(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func containsFold(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			match := true
			for j := 0; j < len(needle); j++ {
				a, b := haystack[i+j], needle[j]
				if a >= 'A' && a <= 'Z' {
					a += 'a' - 'A'
				}
				if b >= 'A' && b <= 'Z' {
					b += 'a' - 'A'
				}
				if a != b {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	})()
}
