package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

// seedFullCase creates a merchant/customer/payment, a RecoveryCase
// (ANALYZED), a RecoveryDiagnosis, and a RecoveryEconomicEvaluation,
// directly through the repositories — bypassing the AI/economic
// pipelines so policy-engine tests can construct exact, controlled
// inputs for each rule.
func seedFullCase(
	t *testing.T, pool *pgxpool.Pool,
	category domain.FailureCategory, action domain.RecommendedAction, confidence float64,
	revenueAtRiskMinorUnits, expectedIncrementalValueMinorUnits int64,
) (*domain.RecoveryCase, *domain.RecoveryDiagnosis, *domain.RecoveryEconomicEvaluation) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	payment := seedPayment(t, pool)

	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusAnalyzed,
		RevenueAtRisk: payment.Amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	diagnosis := &domain.RecoveryDiagnosis{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		FailureCategory: category, DiagnosisReason: "test", CustomerContext: "test",
		RecommendedStrategy: string(action), RecommendedAction: action, RecommendationReason: "test",
		Confidence: confidence, RiskFlags: []string{}, Explanation: "test",
		Provider: "mock", Model: "mock-rule-based-v1", PromptVersion: "v1",
		GeneratedAt: now, CreatedAt: now,
	}
	if err := repository.NewPostgresRecoveryDiagnosisRepository(pool).Create(ctx, diagnosis); err != nil {
		t.Fatalf("create recovery diagnosis: %v", err)
	}

	revenueAtRisk, err := domain.NewMoney(revenueAtRiskMinorUnits, "INR")
	if err != nil {
		t.Fatalf("NewMoney revenueAtRisk: %v", err)
	}
	gross, err := domain.NewMoney(0, "INR")
	if err != nil {
		t.Fatalf("NewMoney gross: %v", err)
	}
	zero, err := domain.NewMoney(0, "INR")
	if err != nil {
		t.Fatalf("NewMoney zero: %v", err)
	}
	evaluation := &domain.RecoveryEconomicEvaluation{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, RecoveryDiagnosisID: diagnosis.ID,
		RecommendedAction: action, RevenueAtRisk: revenueAtRisk,
		RecoveryProbabilityBps: 5000, ExpectedGrossRecovery: gross,
		ActionCost: zero, RiskCost: zero,
		ExpectedIncrementalValueMinorUnits: expectedIncrementalValueMinorUnits,
		EstimatorName:                      "heuristic", EstimatorVersion: "heuristic-v1",
		EconomicModelVersion: "economic-model-v1", CreatedAt: now,
	}
	evalRepo := repository.NewPostgresRecoveryEconomicEvaluationRepository(pool)
	if created, err := evalRepo.TryCreate(ctx, evaluation); err != nil || !created {
		t.Fatalf("create recovery economic evaluation: created=%v err=%v", created, err)
	}

	return recoveryCase, diagnosis, evaluation
}

func newPolicyEngine(pool *pgxpool.Pool) *service.PolicyEngine {
	return service.NewPolicyEngine(pool, service.DefaultPolicyConfig, nil)
}

func TestPolicyEngine_SuccessfulEvaluation_Allow(t *testing.T) {
	pool := testPool(t)
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !outcome.Created {
		t.Fatal("expected Created=true")
	}
	if outcome.Decision.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s (reasons=%v)", outcome.Decision.Outcome, outcome.Decision.ReasonCodes)
	}
	if outcome.Decision.AuthorizedAction != domain.RecommendedActionRetryPayment {
		t.Fatalf("expected AuthorizedAction=retry_payment, got %q", outcome.Decision.AuthorizedAction)
	}
	if outcome.Decision.RecoveryDiagnosisID != diagnosis.ID || outcome.Decision.RecoveryEconomicEvaluationID != evaluation.ID {
		t.Fatal("decision does not reference the exact diagnosis/evaluation it evaluated")
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusAllow {
		t.Fatalf("expected case status ALLOW, got %s", outcome.Case.Status)
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID case: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusAllow {
		t.Fatalf("persisted case status: got %s, want ALLOW", persistedCase.Status)
	}

	assertNoRecoveryActionsCreated(t, pool, recoveryCase.ID)
	assertAuditEventExists(t, pool, recoveryCase.ID, "recovery_policy.evaluated")
}

func TestPolicyEngine_Block(t *testing.T) {
	pool := testPool(t)
	// stop_recovery always BLOCKs.
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryUnknown, domain.RecommendedActionStopRecovery, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if outcome.Decision.Outcome != domain.PolicyDecisionOutcomeBlock {
		t.Fatalf("expected BLOCK, got %s", outcome.Decision.Outcome)
	}
	if outcome.Decision.AuthorizedAction != "" {
		t.Fatalf("expected empty AuthorizedAction for BLOCK, got %q", outcome.Decision.AuthorizedAction)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusBlock {
		t.Fatalf("expected case status BLOCK, got %s", outcome.Case.Status)
	}
	assertNoRecoveryActionsCreated(t, pool, recoveryCase.ID)
}

func TestPolicyEngine_Escalate(t *testing.T) {
	pool := testPool(t)
	// Low confidence always ESCALATEs.
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.05, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if outcome.Decision.Outcome != domain.PolicyDecisionOutcomeEscalate {
		t.Fatalf("expected ESCALATE, got %s", outcome.Decision.Outcome)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusEscalate {
		t.Fatalf("expected case status ESCALATE, got %s", outcome.Case.Status)
	}
	assertNoRecoveryActionsCreated(t, pool, recoveryCase.ID)
}

func TestPolicyEngine_DiagnosisCaseMismatch(t *testing.T) {
	pool := testPool(t)
	caseA, _, evalA := seedFullCase(t, pool, domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.9, 10_000, 5_000)
	_, diagnosisB, _ := seedFullCase(t, pool, domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.9, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	_, err := engine.Evaluate(context.Background(), caseA.ID, diagnosisB.ID, evalA.ID)
	if !errors.Is(err, service.ErrDiagnosisCaseMismatch) {
		t.Fatalf("expected ErrDiagnosisCaseMismatch, got %v", err)
	}
}

func TestPolicyEngine_EvaluationCaseMismatch(t *testing.T) {
	pool := testPool(t)
	caseA, diagnosisA, _ := seedFullCase(t, pool, domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.9, 10_000, 5_000)
	_, _, evalB := seedFullCase(t, pool, domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.9, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	_, err := engine.Evaluate(context.Background(), caseA.ID, diagnosisA.ID, evalB.ID)
	if !errors.Is(err, service.ErrEconomicEvaluationCaseMismatch) {
		t.Fatalf("expected ErrEconomicEvaluationCaseMismatch, got %v", err)
	}
}

func TestPolicyEngine_EvaluationDiagnosisMismatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	recoveryCase, diagnosis1, eval1 := seedFullCase(t, pool, domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.9, 10_000, 5_000)
	_ = eval1

	// A second diagnosis for the SAME case (simulating re-analysis).
	now := time.Now().UTC()
	diagnosis2 := &domain.RecoveryDiagnosis{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		FailureCategory: domain.FailureCategoryTransientFailure, DiagnosisReason: "test2", CustomerContext: "test2",
		RecommendedStrategy: "retry_payment", RecommendedAction: domain.RecommendedActionRetryPayment,
		RecommendationReason: "test2", Confidence: 0.9, RiskFlags: []string{}, Explanation: "test2",
		Provider: "mock", Model: "mock-rule-based-v1", PromptVersion: "v1",
		GeneratedAt: now, CreatedAt: now,
	}
	if err := repository.NewPostgresRecoveryDiagnosisRepository(pool).Create(ctx, diagnosis2); err != nil {
		t.Fatalf("create second diagnosis: %v", err)
	}
	_ = diagnosis1

	engine := newPolicyEngine(pool)
	// eval1 was computed for diagnosis1, not diagnosis2.
	_, err := engine.Evaluate(ctx, recoveryCase.ID, diagnosis2.ID, eval1.ID)
	if !errors.Is(err, service.ErrEconomicEvaluationDiagnosisMismatch) {
		t.Fatalf("expected ErrEconomicEvaluationDiagnosisMismatch, got %v", err)
	}
}

func TestPolicyEngine_MissingEvaluation(t *testing.T) {
	pool := testPool(t)
	recoveryCase, diagnosis, _ := seedFullCase(t, pool, domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.9, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	_, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, uuid.New())
	if !errors.Is(err, service.ErrRecoveryEconomicEvaluationNotFound) {
		t.Fatalf("expected ErrRecoveryEconomicEvaluationNotFound, got %v", err)
	}
}

func TestPolicyEngine_CaseNotFound(t *testing.T) {
	pool := testPool(t)
	engine := newPolicyEngine(pool)
	_, err := engine.Evaluate(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrRecoveryCaseNotFound) {
		t.Fatalf("expected ErrRecoveryCaseNotFound, got %v", err)
	}
}

func TestPolicyEngine_CaseNotAnalyzed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	payment := seedPayment(t, pool)
	now := time.Now().UTC()

	// Case in DETECTED, not ANALYZED.
	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusDetected,
		RevenueAtRisk: payment.Amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}
	diagnosis := &domain.RecoveryDiagnosis{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		FailureCategory: domain.FailureCategoryTransientFailure, DiagnosisReason: "test", CustomerContext: "test",
		RecommendedStrategy: "retry_payment", RecommendedAction: domain.RecommendedActionRetryPayment,
		RecommendationReason: "test", Confidence: 0.9, RiskFlags: []string{}, Explanation: "test",
		Provider: "mock", Model: "mock-rule-based-v1", PromptVersion: "v1",
		GeneratedAt: now, CreatedAt: now,
	}
	if err := repository.NewPostgresRecoveryDiagnosisRepository(pool).Create(ctx, diagnosis); err != nil {
		t.Fatalf("create diagnosis: %v", err)
	}
	revenueAtRisk, _ := domain.NewMoney(10_000, "INR")
	zero, _ := domain.NewMoney(0, "INR")
	evaluation := &domain.RecoveryEconomicEvaluation{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, RecoveryDiagnosisID: diagnosis.ID,
		RecommendedAction: domain.RecommendedActionRetryPayment, RevenueAtRisk: revenueAtRisk,
		RecoveryProbabilityBps: 5000, ExpectedGrossRecovery: zero, ActionCost: zero, RiskCost: zero,
		ExpectedIncrementalValueMinorUnits: 5000, EstimatorName: "heuristic", EstimatorVersion: "heuristic-v1",
		EconomicModelVersion: "economic-model-v1", CreatedAt: now,
	}
	if created, err := repository.NewPostgresRecoveryEconomicEvaluationRepository(pool).TryCreate(ctx, evaluation); err != nil || !created {
		t.Fatalf("create evaluation: created=%v err=%v", created, err)
	}

	engine := newPolicyEngine(pool)
	_, err := engine.Evaluate(ctx, recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if !errors.Is(err, service.ErrRecoveryCaseNotAnalyzed) {
		t.Fatalf("expected ErrRecoveryCaseNotAnalyzed, got %v", err)
	}
}

func TestPolicyEngine_Idempotency(t *testing.T) {
	pool := testPool(t)
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)

	first, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	if !first.Created {
		t.Fatal("expected first call to create a decision")
	}

	second, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if second.Created {
		t.Fatal("expected second call to be a no-op")
	}
	if second.Decision.ID != first.Decision.ID {
		t.Fatalf("expected identical decision ID: first=%s second=%s", first.Decision.ID, second.Decision.ID)
	}
	// Immutability: every field must be identical across both reads.
	if second.Decision.Outcome != first.Decision.Outcome ||
		second.Decision.Explanation != first.Decision.Explanation ||
		second.Decision.PolicyVersion != first.Decision.PolicyVersion {
		t.Fatalf("decision fields changed between reads: first=%+v second=%+v", first.Decision, second.Decision)
	}
	if second.Case.Status != first.Case.Status {
		t.Fatalf("case status differs between idempotent calls: first=%s second=%s", first.Case.Status, second.Case.Status)
	}

	count := countRows(t, pool,
		`SELECT count(*) FROM policy_decisions WHERE recovery_case_id = $1 AND recovery_diagnosis_id = $2 AND recovery_economic_evaluation_id = $3`,
		recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if count != 1 {
		t.Fatalf("expected exactly 1 policy_decisions row after evaluating twice, got %d", count)
	}

	auditCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_policy.evaluated'`,
		recoveryCase.ID)
	if auditCount != 1 {
		t.Fatalf("expected exactly 1 audit row after evaluating twice, got %d", auditCount)
	}
}

func TestPolicyEngine_ConcurrentEvaluationConvergesSafely(t *testing.T) {
	pool := testPool(t)
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)

	const workers = 5
	outcomes := make([]*service.PolicyEvaluationOutcome, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i], errs[i] = engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: Evaluate returned error: %v", i, err)
		}
	}

	count := countRows(t, pool, `SELECT count(*) FROM policy_decisions WHERE recovery_case_id = $1`, recoveryCase.ID)
	if count != 1 {
		t.Fatalf("expected exactly 1 policy_decisions row after concurrent evaluation, got %d", count)
	}

	created := 0
	decisionID := outcomes[0].Decision.ID
	for _, o := range outcomes {
		if o.Decision.ID != decisionID {
			t.Fatalf("expected all workers to converge on the same decision ID")
		}
		if o.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent calls to report Created=true, got %d", workers, created)
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusAllow {
		t.Fatalf("expected final case status ALLOW, got %s", persistedCase.Status)
	}
}

func TestPolicyEngine_GetLatestDecision(t *testing.T) {
	pool := testPool(t)
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)

	if _, err := engine.GetLatestDecision(context.Background(), recoveryCase.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before any decision exists, got %v", err)
	}

	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	latest, err := engine.GetLatestDecision(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetLatestDecision: %v", err)
	}
	if latest.ID != outcome.Decision.ID {
		t.Fatal("expected latest decision to match the one just created")
	}
}

func assertNoRecoveryActionsCreated(t *testing.T, pool *pgxpool.Pool, recoveryCaseID uuid.UUID) {
	t.Helper()
	count := countRows(t, pool, `SELECT count(*) FROM recovery_actions WHERE recovery_case_id = $1`, recoveryCaseID)
	if count != 0 {
		t.Fatalf("expected 0 recovery_actions rows (policy evaluation must never execute), got %d", count)
	}
}

func assertAuditEventExists(t *testing.T, pool *pgxpool.Pool, recoveryCaseID uuid.UUID, eventType string) {
	t.Helper()
	count := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = $2`, recoveryCaseID, eventType)
	if count != 1 {
		t.Fatalf("expected exactly 1 %q audit row, got %d", eventType, count)
	}
}
