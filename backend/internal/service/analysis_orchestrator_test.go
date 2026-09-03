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

func nowUTC() time.Time { return time.Now().UTC() }

// fakeAIClient lets orchestration tests control the AI response without
// any network call, and without depending on ai-service being reachable.
type fakeAIClient struct {
	recommendation *service.AIRecommendation
	err            error
	calls          int
}

func (f *fakeAIClient) Diagnose(ctx context.Context, request service.AIRequest) (*service.AIRecommendation, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.recommendation == nil {
		// Unconfigured fixture: fail loudly rather than return a nil
		// *AIRecommendation, which callers are entitled to assume is
		// non-nil whenever err == nil.
		return nil, errors.New("fakeAIClient: no recommendation configured")
	}
	return f.recommendation, nil
}

func newContextBuilder(pool *pgxpool.Pool) *service.RecoveryContextBuilder {
	return service.NewRecoveryContextBuilder(
		repository.NewPostgresPaymentRepository(pool),
		repository.NewPostgresPaymentAttemptRepository(pool),
		repository.NewPostgresRecoveryActionRepository(pool),
	)
}

func aRecommendationFor(caseID uuid.UUID) *service.AIRecommendation {
	return &service.AIRecommendation{
		CaseID: caseID,
		Diagnosis: service.AIDiagnosis{
			Reason:              "insufficient funds detected",
			FailureCategory:     "insufficient_funds",
			CustomerContext:     "1 attempt",
			RecommendedStrategy: "send_payment_link",
		},
		Recommendation: service.AIRecommendationDetail{
			Action:     "send_payment_link",
			Reason:     "insufficient funds detected",
			Confidence: 0.75,
		},
		RiskFlags:     []string{},
		Explanation:   "test explanation",
		Provider:      "mock",
		Model:         "mock-rule-based-v1",
		PromptVersion: "v1",
		GeneratedAt:   nowUTC(),
	}
}

// TestFullLifecycle_DetectedAnalyzingAnalyzed drives the complete M2+M3
// flow: a qualifying event creates a case (DETECTED -> ANALYZING, per
// Milestone 2), and then AnalyzeCase (Milestone 3) takes it the rest of
// the way to ANALYZED, persisting a RecoveryDiagnosis along the way.
func TestFullLifecycle_DetectedAnalyzingAnalyzed(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)

	ai := &fakeAIClient{}
	analyzer := service.NewAnalysisOrchestrator(pool, newContextBuilder(pool), ai, nil)
	processor := service.NewEventProcessor(pool, analyzer, service.NewLoggingEventPublisher(nil), nil)

	// The fake client needs to know the case ID before it exists, so
	// process the event first to create the case in ANALYZING, then wire
	// the fake's response and re-run analysis directly for clarity of
	// assertion. (EventProcessor already calls AnalyzeCase internally;
	// this two-step form makes the case ID available for the fixture.)
	input := eventInputFor(payment, "payment.failed")
	result, err := processor.Process(context.Background(), input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.RecoveryCase == nil {
		t.Fatal("expected a recovery case")
	}

	// Without a wired recommendation, AnalyzeCase (called internally by
	// Process above) will have failed to validate a zero-value
	// AIRecommendation and left the case at ANALYZING. Confirm that, then
	// wire a real recommendation and analyze again explicitly.
	if result.RecoveryCase.Status != domain.RecoveryCaseStatusAnalyzing {
		t.Fatalf("expected case to remain ANALYZING before a usable AI response, got %s", result.RecoveryCase.Status)
	}

	ai.recommendation = aRecommendationFor(result.RecoveryCase.ID)
	outcome, err := analyzer.AnalyzeCase(context.Background(), result.RecoveryCase.ID, "payment.failed")
	if err != nil {
		t.Fatalf("AnalyzeCase: %v", err)
	}
	if !outcome.Analyzed {
		t.Fatal("expected Analyzed=true")
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusAnalyzed {
		t.Fatalf("expected case status ANALYZED, got %s", outcome.Case.Status)
	}
	if outcome.Diagnosis == nil {
		t.Fatal("expected a persisted diagnosis")
	}
	if outcome.Diagnosis.RecommendedAction != domain.RecommendedActionSendPaymentLink {
		t.Fatalf("unexpected recommended action: %s", outcome.Diagnosis.RecommendedAction)
	}

	// Verify durable state directly.
	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), result.RecoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusAnalyzed {
		t.Fatalf("persisted case status: got %s, want ANALYZED", persistedCase.Status)
	}

	diagnoses, err := repository.NewPostgresRecoveryDiagnosisRepository(pool).ListByRecoveryCaseID(context.Background(), result.RecoveryCase.ID)
	if err != nil {
		t.Fatalf("ListByRecoveryCaseID: %v", err)
	}
	if len(diagnoses) != 1 {
		t.Fatalf("expected 1 persisted diagnosis, got %d", len(diagnoses))
	}
}

// TestAnalyzeCase_AIFailureLeavesCaseInAnalyzing verifies that when the AI
// call fails, the case is never incorrectly advanced to ANALYZED (or any
// other state) — an analysis failure is not a payment failure, a recovery
// failure, or a successful recovery.
func TestAnalyzeCase_AIFailureLeavesCaseInAnalyzing(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)

	ai := &fakeAIClient{err: service.ErrDiagnosisFailed}
	analyzer := service.NewAnalysisOrchestrator(pool, newContextBuilder(pool), ai, nil)
	processor := service.NewEventProcessor(pool, analyzer, service.NewLoggingEventPublisher(nil), nil)

	input := eventInputFor(payment, "payment.failed")
	result, err := processor.Process(context.Background(), input)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.RecoveryCase == nil {
		t.Fatal("expected a recovery case")
	}
	if result.Analyzed {
		t.Fatal("expected Analyzed=false when the AI call fails")
	}
	if result.AnalysisError == "" {
		t.Fatal("expected AnalysisError to be set")
	}
	if result.RecoveryCase.Status != domain.RecoveryCaseStatusAnalyzing {
		t.Fatalf("expected case status ANALYZING after AI failure, got %s", result.RecoveryCase.Status)
	}

	// Verify durable state directly: no diagnosis was persisted, no
	// transition occurred.
	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), result.RecoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusAnalyzing {
		t.Fatalf("persisted case status: got %s, want ANALYZING", persistedCase.Status)
	}

	diagnoses, err := repository.NewPostgresRecoveryDiagnosisRepository(pool).ListByRecoveryCaseID(context.Background(), result.RecoveryCase.ID)
	if err != nil {
		t.Fatalf("ListByRecoveryCaseID: %v", err)
	}
	if len(diagnoses) != 0 {
		t.Fatalf("expected 0 persisted diagnoses after AI failure, got %d", len(diagnoses))
	}
}

// TestAnalyzeCase_NotFoundCase verifies a nonexistent case is reported
// clearly rather than as a generic error.
func TestAnalyzeCase_NotFoundCase(t *testing.T) {
	pool := testPool(t)
	analyzer := service.NewAnalysisOrchestrator(pool, newContextBuilder(pool), &fakeAIClient{}, nil)

	_, err := analyzer.AnalyzeCase(context.Background(), uuid.New(), "payment.failed")
	if !errors.Is(err, service.ErrRecoveryCaseNotFound) {
		t.Fatalf("expected ErrRecoveryCaseNotFound, got %v", err)
	}
}

// TestAnalyzeCase_NoopWhenNotAnalyzing verifies repeated/out-of-order
// analysis calls are safe: a case that is not in ANALYZING is left
// completely untouched.
func TestAnalyzeCase_NoopWhenNotAnalyzing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	payment := seedPayment(t, pool)

	amount, _ := domain.NewMoney(49950, "INR")
	now := nowUTC()
	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusDetected, // not ANALYZING
		RevenueAtRisk: amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	ai := &fakeAIClient{recommendation: aRecommendationFor(recoveryCase.ID)}
	analyzer := service.NewAnalysisOrchestrator(pool, newContextBuilder(pool), ai, nil)

	outcome, err := analyzer.AnalyzeCase(ctx, recoveryCase.ID, "payment.failed")
	if err != nil {
		t.Fatalf("AnalyzeCase: %v", err)
	}
	if outcome.Analyzed {
		t.Fatal("expected Analyzed=false for a case not in ANALYZING")
	}
	if ai.calls != 0 {
		t.Fatalf("expected the AI client to not be called, got %d calls", ai.calls)
	}

	persisted, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(ctx, recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.Status != domain.RecoveryCaseStatusDetected {
		t.Fatalf("case status changed unexpectedly: got %s, want DETECTED", persisted.Status)
	}
}
