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

// seedAllowDecision drives a real PolicyEngine evaluation to produce a
// genuine ALLOW PolicyDecision for retry_payment, rather than hand-crafting
// one — so ExecutionEngine tests exercise the real authorization boundary.
func seedAllowDecision(t *testing.T, pool *pgxpool.Pool) (*domain.RecoveryCase, *domain.PolicyDecision) {
	t.Helper()
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if outcome.Decision.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s", outcome.Decision.Outcome)
	}
	return outcome.Case, outcome.Decision
}

func seedBlockDecision(t *testing.T, pool *pgxpool.Pool) (*domain.RecoveryCase, *domain.PolicyDecision) {
	t.Helper()
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
	return outcome.Case, outcome.Decision
}

func seedEscalateDecision(t *testing.T, pool *pgxpool.Pool) (*domain.RecoveryCase, *domain.PolicyDecision) {
	t.Helper()
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
	return outcome.Case, outcome.Decision
}

// seedAllowDecisionForAction produces a genuine ALLOW decision whose
// AuthorizedAction is send_payment_link (auto-allowed by DefaultPolicyConfig)
// rather than retry_payment — used to test the "action not executable"
// rejection, since ExecutionEngine only implements retry_payment.
func seedAllowDecisionForSendPaymentLink(t *testing.T, pool *pgxpool.Pool) (*domain.RecoveryCase, *domain.PolicyDecision) {
	t.Helper()
	recoveryCase, diagnosis, evaluation := seedFullCase(t, pool,
		domain.FailureCategoryCustomerAbandonment, domain.RecommendedActionSendPaymentLink, 0.90, 10_000, 5_000)

	engine := newPolicyEngine(pool)
	outcome, err := engine.Evaluate(context.Background(), recoveryCase.ID, diagnosis.ID, evaluation.ID)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if outcome.Decision.Outcome != domain.PolicyDecisionOutcomeAllow {
		t.Fatalf("expected ALLOW, got %s (reasons=%v)", outcome.Decision.Outcome, outcome.Decision.ReasonCodes)
	}
	if outcome.Decision.AuthorizedAction != domain.RecommendedActionSendPaymentLink {
		t.Fatalf("expected AuthorizedAction=send_payment_link, got %q", outcome.Decision.AuthorizedAction)
	}
	return outcome.Case, outcome.Decision
}

func newExecutionEngine(pool *pgxpool.Pool, provider service.PaymentProvider) *service.ExecutionEngine {
	return service.NewExecutionEngine(pool, provider, nil)
}

func assertRecoveryActionCount(t *testing.T, pool *pgxpool.Pool, recoveryCaseID uuid.UUID, want int) {
	t.Helper()
	got := countRows(t, pool, `SELECT count(*) FROM recovery_actions WHERE recovery_case_id = $1`, recoveryCaseID)
	if got != want {
		t.Fatalf("expected %d recovery_actions rows, got %d", want, got)
	}
}

func TestExecutionEngine_AllowExecutesSuccessfully(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	outcome, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !outcome.Created {
		t.Fatal("expected Created=true")
	}
	if outcome.Action.Status != domain.RecoveryActionStatusSucceeded {
		t.Fatalf("expected SUCCEEDED, got %s", outcome.Action.Status)
	}
	if outcome.Action.ActionType != domain.RecoveryActionTypeRetryPayment {
		t.Fatalf("expected retry_payment action type, got %s", outcome.Action.ActionType)
	}
	if outcome.Action.Provider != "fake" {
		t.Fatalf("expected provider=fake, got %q", outcome.Action.Provider)
	}
	if outcome.Action.ProviderReference == "" {
		t.Fatal("expected a provider reference on success")
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case status VERIFYING, got %s", outcome.Case.Status)
	}
	if provider.InvocationCount() != 1 {
		t.Fatalf("expected exactly 1 provider invocation, got %d", provider.InvocationCount())
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("persisted case status: got %s, want VERIFYING", persistedCase.Status)
	}
	assertRecoveryActionCount(t, pool, recoveryCase.ID, 1)

	startedCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_execution.started'`, recoveryCase.ID)
	if startedCount != 1 {
		t.Fatalf("expected 1 recovery_execution.started audit row, got %d", startedCount)
	}
	completedCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_execution.completed'`, recoveryCase.ID)
	if completedCount != 1 {
		t.Fatalf("expected 1 recovery_execution.completed audit row, got %d", completedCount)
	}
}

func TestExecutionEngine_DefinitiveFailure(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioDefinitiveFailure)
	engine := newExecutionEngine(pool, provider)

	outcome, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Action.Status != domain.RecoveryActionStatusFailed {
		t.Fatalf("expected FAILED, got %s", outcome.Action.Status)
	}
	if outcome.Action.ErrorCode == "" {
		t.Fatal("expected a non-empty error code")
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case status VERIFYING even on failure, got %s", outcome.Case.Status)
	}
}

func TestExecutionEngine_TimeoutBecomesUnknown(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioTimeout)
	engine := newExecutionEngine(pool, provider)

	outcome, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Action.Status != domain.RecoveryActionStatusUnknown {
		t.Fatalf("expected UNKNOWN, got %s", outcome.Action.Status)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case status VERIFYING, got %s", outcome.Case.Status)
	}
	if outcome.Case.Status == domain.RecoveryCaseStatusSuccess || outcome.Case.Status == domain.RecoveryCaseStatusFailed {
		t.Fatal("an ambiguous provider outcome must never be reported as SUCCESS or FAILED")
	}

	unknownCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_execution.unknown'`, recoveryCase.ID)
	if unknownCount != 1 {
		t.Fatalf("expected 1 recovery_execution.unknown audit row, got %d", unknownCount)
	}
}

func TestExecutionEngine_TransportErrorBecomesUnknown(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioTransportError)
	engine := newExecutionEngine(pool, provider)

	outcome, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Action.Status != domain.RecoveryActionStatusUnknown {
		t.Fatalf("expected UNKNOWN, got %s", outcome.Action.Status)
	}
}

func TestExecutionEngine_BlockCannotExecute(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedBlockDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	_, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if !errors.Is(err, service.ErrPolicyDecisionNotAllow) {
		t.Fatalf("expected ErrPolicyDecisionNotAllow, got %v", err)
	}
	if provider.InvocationCount() != 0 {
		t.Fatal("expected zero provider invocations for a BLOCK decision")
	}
	assertRecoveryActionCount(t, pool, recoveryCase.ID, 0)
}

func TestExecutionEngine_EscalateCannotExecute(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedEscalateDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	_, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if !errors.Is(err, service.ErrPolicyDecisionNotAllow) {
		t.Fatalf("expected ErrPolicyDecisionNotAllow, got %v", err)
	}
	if provider.InvocationCount() != 0 {
		t.Fatal("expected zero provider invocations for an ESCALATE decision")
	}
	assertRecoveryActionCount(t, pool, recoveryCase.ID, 0)
}

func TestExecutionEngine_AnalyzedCaseCannotExecute(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	payment := seedPayment(t, pool)
	now := time.Now().UTC()

	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusAnalyzed,
		RevenueAtRisk: payment.Amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	// A structurally-valid ALLOW decision that references this case, even
	// though the case itself never actually reached ALLOW (simulating,
	// e.g., a stale/replayed decision ID against a case that moved on).
	decision := &domain.PolicyDecision{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		RecoveryDiagnosisID: uuid.New(), RecoveryEconomicEvaluationID: uuid.New(),
		Outcome: domain.PolicyDecisionOutcomeAllow, AuthorizedAction: domain.RecommendedActionRetryPayment,
		PolicyVersion: service.PolicyVersion, ReasonCodes: []domain.PolicyReasonCode{domain.PolicyReasonPolicyAllowed},
		Explanation: "test", EvaluatedAt: now, CreatedAt: now,
	}
	if created, err := repository.NewPostgresPolicyDecisionRepository(pool).TryCreate(ctx, decision); err != nil || !created {
		t.Fatalf("create policy decision: created=%v err=%v", created, err)
	}

	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	_, err := engine.Execute(ctx, recoveryCase.ID, decision.ID)
	if !errors.Is(err, service.ErrRecoveryCaseNotAllow) {
		t.Fatalf("expected ErrRecoveryCaseNotAllow, got %v", err)
	}
	if provider.InvocationCount() != 0 {
		t.Fatal("expected zero provider invocations")
	}
}

func TestExecutionEngine_MissingPolicyDecision(t *testing.T) {
	pool := testPool(t)
	recoveryCase, _ := seedAllowDecision(t, pool)
	engine := newExecutionEngine(pool, service.NewFakeProvider(service.FakeProviderScenarioSuccess))

	_, err := engine.Execute(context.Background(), recoveryCase.ID, uuid.New())
	if !errors.Is(err, service.ErrPolicyDecisionNotFound) {
		t.Fatalf("expected ErrPolicyDecisionNotFound, got %v", err)
	}
}

func TestExecutionEngine_PolicyDecisionBelongsToAnotherCase(t *testing.T) {
	pool := testPool(t)
	caseA, decisionA := seedAllowDecision(t, pool)
	caseB, _ := seedAllowDecision(t, pool)
	_ = caseA

	engine := newExecutionEngine(pool, service.NewFakeProvider(service.FakeProviderScenarioSuccess))
	_, err := engine.Execute(context.Background(), caseB.ID, decisionA.ID)
	if !errors.Is(err, service.ErrPolicyDecisionCaseMismatch) {
		t.Fatalf("expected ErrPolicyDecisionCaseMismatch, got %v", err)
	}
}

func TestExecutionEngine_AllowWithoutAuthorizedAction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	recoveryCase, _, _ := seedFullCase(t, pool,
		domain.FailureCategoryTransientFailure, domain.RecommendedActionRetryPayment, 0.90, 10_000, 5_000)
	// Force the case into ALLOW directly (bypassing PolicyEngine) so we can
	// pair it with a decision that has no AuthorizedAction — an invariant
	// PolicyEngine itself should never violate, but ExecutionEngine must
	// still check defensively rather than trust it blindly.
	now := time.Now().UTC()
	if err := repository.NewPostgresRecoveryCaseRepository(pool).UpdateStatus(ctx, recoveryCase.ID, domain.RecoveryCaseStatusAnalyzed, domain.RecoveryCaseStatusPolicyCheck, now); err != nil {
		t.Fatalf("transition to POLICY_CHECK: %v", err)
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).UpdateStatus(ctx, recoveryCase.ID, domain.RecoveryCaseStatusPolicyCheck, domain.RecoveryCaseStatusAllow, now); err != nil {
		t.Fatalf("transition to ALLOW: %v", err)
	}

	decision := &domain.PolicyDecision{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID,
		RecoveryDiagnosisID: uuid.New(), RecoveryEconomicEvaluationID: uuid.New(),
		Outcome: domain.PolicyDecisionOutcomeAllow, AuthorizedAction: "",
		PolicyVersion: service.PolicyVersion, ReasonCodes: []domain.PolicyReasonCode{domain.PolicyReasonPolicyAllowed},
		Explanation: "test", EvaluatedAt: now, CreatedAt: now,
	}
	if created, err := repository.NewPostgresPolicyDecisionRepository(pool).TryCreate(ctx, decision); err != nil || !created {
		t.Fatalf("create policy decision: created=%v err=%v", created, err)
	}

	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	_, err := engine.Execute(ctx, recoveryCase.ID, decision.ID)
	if !errors.Is(err, service.ErrMissingAuthorizedAction) {
		t.Fatalf("expected ErrMissingAuthorizedAction, got %v", err)
	}
	if provider.InvocationCount() != 0 {
		t.Fatal("expected zero provider invocations")
	}
}

func TestExecutionEngine_UnsupportedActionNoProviderCall(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecisionForSendPaymentLink(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	_, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if !errors.Is(err, service.ErrActionNotExecutable) {
		t.Fatalf("expected ErrActionNotExecutable, got %v", err)
	}
	if provider.InvocationCount() != 0 {
		t.Fatal("expected zero provider invocations for an unsupported action")
	}
	assertRecoveryActionCount(t, pool, recoveryCase.ID, 0)
}

func TestExecutionEngine_DuplicateExecutionRequestIsIdempotent(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	first, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if !first.Created {
		t.Fatal("expected first call to be a fresh execution")
	}

	second, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second.Created {
		t.Fatal("expected second call to be an idempotent no-op")
	}
	if second.Action.ID != first.Action.ID {
		t.Fatal("expected the same RecoveryAction ID on retry")
	}
	if provider.InvocationCount() != 1 {
		t.Fatalf("expected exactly 1 provider invocation across both calls, got %d", provider.InvocationCount())
	}
	assertRecoveryActionCount(t, pool, recoveryCase.ID, 1)
}

func TestExecutionEngine_ConcurrentExecutionDoesNotDuplicate(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	const workers = 5
	outcomes := make([]*service.ExecutionOutcome, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i], errs[i] = engine.Execute(context.Background(), recoveryCase.ID, decision.ID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: Execute returned error: %v", i, err)
		}
	}

	if provider.InvocationCount() != 1 {
		t.Fatalf("expected exactly 1 provider invocation across %d concurrent callers, got %d", workers, provider.InvocationCount())
	}
	assertRecoveryActionCount(t, pool, recoveryCase.ID, 1)

	actionID := outcomes[0].Action.ID
	createdCount := 0
	for _, o := range outcomes {
		if o.Action.ID != actionID {
			t.Fatal("expected all workers to converge on the same recovery action ID")
		}
		if o.Created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent calls to report Created=true, got %d", workers, createdCount)
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected final case status VERIFYING, got %s", persistedCase.Status)
	}
}

func TestExecutionEngine_NoSecretsInPersistedMetadata(t *testing.T) {
	pool := testPool(t)
	recoveryCase, decision := seedAllowDecision(t, pool)
	provider := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	engine := newExecutionEngine(pool, provider)

	if _, err := engine.Execute(context.Background(), recoveryCase.ID, decision.ID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	forbidden := []string{"card_number", "cvv", "api_key", "secret", "password", "key_secret", "authorization"}

	actions, err := repository.NewPostgresRecoveryActionRepository(pool).ListByRecoveryCaseID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("ListByRecoveryCaseID: %v", err)
	}
	for _, a := range actions {
		body := string(a.ExecutionMetadata)
		for _, f := range forbidden {
			if containsFold(body, f) {
				t.Fatalf("recovery_action execution_metadata contains forbidden substring %q: %s", f, body)
			}
		}
	}

	var auditBodies []string
	rows, err := pool.Query(context.Background(), `SELECT metadata FROM audit_events WHERE recovery_case_id = $1`, recoveryCase.ID)
	if err != nil {
		t.Fatalf("query audit_events: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var metadata []byte
		if err := rows.Scan(&metadata); err != nil {
			t.Fatalf("scan audit metadata: %v", err)
		}
		auditBodies = append(auditBodies, string(metadata))
	}
	for _, body := range auditBodies {
		for _, f := range forbidden {
			if containsFold(body, f) {
				t.Fatalf("audit_events metadata contains forbidden substring %q: %s", f, body)
			}
		}
	}
}

func containsFold(haystack, needle string) bool {
	h := []rune(haystack)
	n := []rune(needle)
	toLower := func(rs []rune) []rune {
		out := make([]rune, len(rs))
		for i, r := range rs {
			if r >= 'A' && r <= 'Z' {
				r = r - 'A' + 'a'
			}
			out[i] = r
		}
		return out
	}
	h = toLower(h)
	n = toLower(n)
	if len(n) == 0 || len(n) > len(h) {
		return len(n) == 0
	}
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
