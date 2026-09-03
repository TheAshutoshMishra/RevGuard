package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

// seedCaseAndDiagnosis creates a RecoveryCase (ANALYZING) for payment and
// a RecoveryDiagnosis for that case, directly through the repositories —
// bypassing event ingestion/AI analysis so economic-engine tests can
// focus purely on evaluation.
func seedCaseAndDiagnosis(t *testing.T, pool *pgxpool.Pool, payment *domain.Payment, category domain.FailureCategory, action domain.RecommendedAction) (*domain.RecoveryCase, *domain.RecoveryDiagnosis) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusAnalyzing,
		RevenueAtRisk: payment.Amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	diagnosis := &domain.RecoveryDiagnosis{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		FailureCategory: category, DiagnosisReason: "test", CustomerContext: "test",
		RecommendedStrategy: string(action), RecommendedAction: action, RecommendationReason: "test",
		Confidence: 0.7, RiskFlags: []string{}, Explanation: "test",
		Provider: "mock", Model: "mock-rule-based-v1", PromptVersion: "v1",
		GeneratedAt: now, CreatedAt: now,
	}
	if err := repository.NewPostgresRecoveryDiagnosisRepository(pool).Create(ctx, diagnosis); err != nil {
		t.Fatalf("create recovery diagnosis: %v", err)
	}
	return recoveryCase, diagnosis
}

func newEconomicEngine(pool *pgxpool.Pool) *service.EconomicEngine {
	return service.NewEconomicEngine(pool, service.NewHeuristicProbabilityEstimator(), nil)
}

func TestEconomicEngine_FullEvaluationFlow(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	recoveryCase, diagnosis := seedCaseAndDiagnosis(t, pool, payment,
		domain.FailureCategoryInsufficientFunds, domain.RecommendedActionSendPaymentLink)

	engine := newEconomicEngine(pool)
	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !outcome.Created {
		t.Fatal("expected Created=true for a fresh evaluation")
	}

	eval := outcome.Evaluation
	if eval.RecoveryCaseID != recoveryCase.ID {
		t.Errorf("RecoveryCaseID mismatch")
	}
	if eval.RecoveryDiagnosisID != diagnosis.ID {
		t.Errorf("RecoveryDiagnosisID mismatch")
	}
	if eval.RecommendedAction != domain.RecommendedActionSendPaymentLink {
		t.Errorf("RecommendedAction mismatch: got %s", eval.RecommendedAction)
	}
	if eval.RevenueAtRisk.MinorUnits != payment.Amount.MinorUnits || eval.RevenueAtRisk.Currency != payment.Amount.Currency {
		t.Errorf("RevenueAtRisk mismatch: got %+v", eval.RevenueAtRisk)
	}
	if eval.RecoveryProbabilityBps <= 0 || eval.RecoveryProbabilityBps > domain.MaxProbabilityBasisPoints {
		t.Errorf("probability out of expected range: %d", eval.RecoveryProbabilityBps)
	}
	wantGross := eval.RevenueAtRisk.MinorUnits * int64(eval.RecoveryProbabilityBps) / int64(domain.MaxProbabilityBasisPoints)
	if eval.ExpectedGrossRecovery.MinorUnits != wantGross {
		t.Errorf("ExpectedGrossRecovery mismatch: got %d, want %d", eval.ExpectedGrossRecovery.MinorUnits, wantGross)
	}
	wantIncremental := eval.ExpectedGrossRecovery.MinorUnits - eval.ActionCost.MinorUnits - eval.RiskCost.MinorUnits
	if eval.ExpectedIncrementalValueMinorUnits != wantIncremental {
		t.Errorf("ExpectedIncrementalValueMinorUnits mismatch: got %d, want %d", eval.ExpectedIncrementalValueMinorUnits, wantIncremental)
	}
	if eval.EstimatorName != "heuristic" || eval.EstimatorVersion != service.HeuristicEstimatorVersion {
		t.Errorf("estimator metadata mismatch: %s/%s", eval.EstimatorName, eval.EstimatorVersion)
	}
	if eval.EconomicModelVersion != service.EconomicModelVersion {
		t.Errorf("economic_model_version mismatch: got %s", eval.EconomicModelVersion)
	}

	// Retrieve independently and confirm it round-trips.
	persisted, err := repository.NewPostgresRecoveryEconomicEvaluationRepository(pool).GetByID(context.Background(), eval.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.ExpectedIncrementalValueMinorUnits != eval.ExpectedIncrementalValueMinorUnits {
		t.Errorf("persisted incremental value mismatch")
	}

	// Verify the RecoveryCase status was NOT changed by evaluation.
	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID case: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusAnalyzing {
		t.Fatalf("economic evaluation must not change case status: got %s, want ANALYZING (unchanged)", persistedCase.Status)
	}

	// Verify the audit event.
	auditCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_economics.evaluated'`,
		recoveryCase.ID)
	if auditCount != 1 {
		t.Fatalf("expected 1 recovery_economics.evaluated audit row, got %d", auditCount)
	}
}

func TestEconomicEngine_Idempotency(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	recoveryCase, diagnosis := seedCaseAndDiagnosis(t, pool, payment,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)

	engine := newEconomicEngine(pool)

	first, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID)
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	if !first.Created {
		t.Fatal("expected first call to create a new evaluation")
	}

	second, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID)
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if second.Created {
		t.Fatal("expected second call to be a no-op (Created=false)")
	}
	if second.Evaluation.ID != first.Evaluation.ID {
		t.Fatalf("expected the same evaluation ID: first=%s second=%s", first.Evaluation.ID, second.Evaluation.ID)
	}

	count := countRows(t, pool,
		`SELECT count(*) FROM recovery_economic_evaluations WHERE recovery_diagnosis_id = $1`, diagnosis.ID)
	if count != 1 {
		t.Fatalf("expected exactly 1 evaluation row after evaluating twice, got %d", count)
	}

	auditCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_economics.evaluated'`,
		recoveryCase.ID)
	if auditCount != 1 {
		t.Fatalf("expected exactly 1 audit row after evaluating twice, got %d", auditCount)
	}
}

func TestEconomicEngine_CaseNotFound(t *testing.T) {
	pool := testPool(t)
	engine := newEconomicEngine(pool)

	_, err := engine.Evaluate(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrRecoveryCaseNotFound) {
		t.Fatalf("expected ErrRecoveryCaseNotFound, got %v", err)
	}
}

func TestEconomicEngine_DiagnosisNotFound(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	recoveryCase, _ := seedCaseAndDiagnosis(t, pool, payment,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)

	engine := newEconomicEngine(pool)
	_, err := engine.Evaluate(context.Background(), recoveryCase.ID, uuid.New())
	if !errors.Is(err, service.ErrRecoveryDiagnosisNotFound) {
		t.Fatalf("expected ErrRecoveryDiagnosisNotFound, got %v", err)
	}
}

func TestEconomicEngine_DiagnosisCaseMismatch(t *testing.T) {
	pool := testPool(t)
	paymentA := seedPayment(t, pool)
	paymentB := seedPayment(t, pool)
	caseA, _ := seedCaseAndDiagnosis(t, pool, paymentA,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)
	_, diagnosisB := seedCaseAndDiagnosis(t, pool, paymentB,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)

	engine := newEconomicEngine(pool)
	// caseA with diagnosisB's ID: diagnosisB belongs to a different case.
	_, err := engine.Evaluate(context.Background(), caseA.ID, diagnosisB.ID)
	if !errors.Is(err, service.ErrDiagnosisCaseMismatch) {
		t.Fatalf("expected ErrDiagnosisCaseMismatch, got %v", err)
	}
}

func TestEconomicEngine_DifferentDiagnosesGetSeparateEvaluations(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	recoveryCase, diagnosis1 := seedCaseAndDiagnosis(t, pool, payment,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)

	// Simulate a second, later diagnosis for the same case (e.g.
	// re-analysis) — a new diagnosis row, same case.
	ctx := context.Background()
	now := time.Now().UTC()
	diagnosis2 := &domain.RecoveryDiagnosis{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		FailureCategory: domain.FailureCategoryMandateIssue, DiagnosisReason: "test2", CustomerContext: "test2",
		RecommendedStrategy: "escalate_to_human", RecommendedAction: domain.RecommendedActionEscalateToHuman,
		RecommendationReason: "test2", Confidence: 0.6, RiskFlags: []string{}, Explanation: "test2",
		Provider: "mock", Model: "mock-rule-based-v1", PromptVersion: "v1",
		GeneratedAt: now, CreatedAt: now,
	}
	if err := repository.NewPostgresRecoveryDiagnosisRepository(pool).Create(ctx, diagnosis2); err != nil {
		t.Fatalf("create second diagnosis: %v", err)
	}

	engine := newEconomicEngine(pool)
	outcome1, err := engine.Evaluate(ctx, recoveryCase.ID, diagnosis1.ID)
	if err != nil {
		t.Fatalf("Evaluate diagnosis1: %v", err)
	}
	outcome2, err := engine.Evaluate(ctx, recoveryCase.ID, diagnosis2.ID)
	if err != nil {
		t.Fatalf("Evaluate diagnosis2: %v", err)
	}

	if outcome1.Evaluation.ID == outcome2.Evaluation.ID {
		t.Fatal("expected two distinct evaluations for two distinct diagnoses")
	}
	if !outcome1.Created || !outcome2.Created {
		t.Fatal("expected both evaluations to be newly created")
	}

	count := countRows(t, pool, `SELECT count(*) FROM recovery_economic_evaluations WHERE recovery_case_id = $1`, recoveryCase.ID)
	if count != 2 {
		t.Fatalf("expected 2 evaluation rows for the case, got %d", count)
	}
}

func TestEconomicEngine_GetLatestEvaluation(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	recoveryCase, diagnosis := seedCaseAndDiagnosis(t, pool, payment,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment)

	engine := newEconomicEngine(pool)

	if _, err := engine.GetLatestEvaluation(context.Background(), recoveryCase.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before any evaluation exists, got %v", err)
	}

	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	latest, err := engine.GetLatestEvaluation(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetLatestEvaluation: %v", err)
	}
	if latest.ID != outcome.Evaluation.ID {
		t.Fatalf("expected latest evaluation to match the one just created")
	}
}
