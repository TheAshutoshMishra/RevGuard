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

func newReconciliationEngine(pool *pgxpool.Pool, reconciler service.PaymentReconciler) *service.ReconciliationEngine {
	return service.NewReconciliationEngine(pool, reconciler, nil)
}

func TestReconciliationEngine_CapturedEstablishesSuccess(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusUnknown)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 49950, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !outcome.Applied {
		t.Fatal("expected the outcome to be applied")
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusSuccess {
		t.Fatalf("expected SUCCESS, got %s", outcome.Case.Status)
	}

	recOutcome, err := repository.NewPostgresRecoveryOutcomeRepository(pool).GetByRecoveryActionID(context.Background(), action.ID)
	if err != nil {
		t.Fatalf("GetByRecoveryActionID: %v", err)
	}
	if recOutcome.Status != domain.RecoveryOutcomeStatusSuccess {
		t.Fatalf("expected outcome SUCCESS, got %s", recOutcome.Status)
	}
	if recOutcome.RecoveredAmount.MinorUnits != 49950 || recOutcome.RecoveredAmount.Currency != "INR" {
		t.Fatalf("unexpected recovered amount: %d %s", recOutcome.RecoveredAmount.MinorUnits, recOutcome.RecoveredAmount.Currency)
	}
	if recOutcome.Source != domain.RecoveryOutcomeSourceReconciliation {
		t.Fatalf("expected source RECONCILIATION, got %s", recOutcome.Source)
	}
	if recOutcome.Provider != "razorpay" {
		t.Fatalf("expected provider razorpay, got %q", recOutcome.Provider)
	}

	persistedAction, err := repository.NewPostgresRecoveryActionRepository(pool).GetByID(context.Background(), action.ID)
	if err != nil {
		t.Fatalf("GetByID action: %v", err)
	}
	if persistedAction.Status != domain.RecoveryActionStatusSucceeded {
		t.Fatalf("expected action resolved to SUCCEEDED, got %s", persistedAction.Status)
	}

	if reconciler.InvocationCount() != 1 {
		t.Fatalf("expected exactly 1 reconciler invocation, got %d", reconciler.InvocationCount())
	}
}

func TestReconciliationEngine_FailedFromProviderEstablishesFailed(t *testing.T) {
	pool := testPool(t)
	// Execution accepted the retry (link created), but the provider
	// reports the payment link was never paid — "action executed
	// successfully" != "revenue recovered."
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentFailed, 0, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusFailed {
		t.Fatalf("expected FAILED, got %s", outcome.Case.Status)
	}

	recOutcome, err := repository.NewPostgresRecoveryOutcomeRepository(pool).GetByRecoveryActionID(context.Background(), action.ID)
	if err != nil {
		t.Fatalf("GetByRecoveryActionID: %v", err)
	}
	if recOutcome.Status != domain.RecoveryOutcomeStatusFailed {
		t.Fatalf("expected outcome FAILED, got %s", recOutcome.Status)
	}
	if recOutcome.RecoveredAmount.MinorUnits != 0 {
		t.Fatalf("expected zero recovered amount, got %d", recOutcome.RecoveredAmount.MinorUnits)
	}
}

func TestReconciliationEngine_KnownExecutionFailureNeedsNoProviderCall(t *testing.T) {
	pool := testPool(t)
	// The RecoveryAction itself already definitively FAILED at execution
	// time (Milestone 6) — no external call should be needed at all.
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusFailed)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 99999, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusFailed {
		t.Fatalf("expected FAILED, got %s", outcome.Case.Status)
	}
	if reconciler.InvocationCount() != 0 {
		t.Fatalf("expected zero reconciler invocations for an already-known failure, got %d", reconciler.InvocationCount())
	}

	recOutcome, err := repository.NewPostgresRecoveryOutcomeRepository(pool).GetByRecoveryActionID(context.Background(), action.ID)
	if err != nil {
		t.Fatalf("GetByRecoveryActionID: %v", err)
	}
	if recOutcome.Status != domain.RecoveryOutcomeStatusFailed {
		t.Fatalf("expected outcome FAILED, got %s", recOutcome.Status)
	}
}

func TestReconciliationEngine_PendingStaysVerifying(t *testing.T) {
	pool := testPool(t)
	recoveryCase, _ := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusUnknown)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentPending, 0, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if outcome.Applied {
		t.Fatal("expected no outcome to be applied for PENDING")
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case to remain VERIFYING, got %s", outcome.Case.Status)
	}
	assertOutcomeCount(t, pool, recoveryCase.ID, 0)
}

func TestReconciliationEngine_AmbiguousErrorStaysVerifyingNoFabrication(t *testing.T) {
	pool := testPool(t)

	for _, scenario := range []service.FakeReconcilerScenario{
		service.FakeReconcilerScenarioProviderTimeout,
		service.FakeReconcilerScenarioTransportError,
	} {
		recoveryCase, _ := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusUnknown)
		reconciler := service.NewFakeReconciler(scenario, 0, "INR")
		engine := newReconciliationEngine(pool, reconciler)

		outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
		if err != nil {
			t.Fatalf("Reconcile(%s): %v", scenario, err)
		}
		if outcome.Applied {
			t.Fatalf("expected no outcome to be applied for ambiguous scenario %s", scenario)
		}
		if outcome.Case.Status != domain.RecoveryCaseStatusVerifying {
			t.Fatalf("expected case to remain VERIFYING for %s, got %s", scenario, outcome.Case.Status)
		}
		assertOutcomeCount(t, pool, recoveryCase.ID, 0)
	}
}

func TestReconciliationEngine_ReferenceNotFoundResolvesToUnknown(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusUnknown)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentNotFound, 0, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusUnknown {
		t.Fatalf("expected UNKNOWN, got %s", outcome.Case.Status)
	}

	recOutcome, err := repository.NewPostgresRecoveryOutcomeRepository(pool).GetByRecoveryActionID(context.Background(), action.ID)
	if err != nil {
		t.Fatalf("GetByRecoveryActionID: %v", err)
	}
	if recOutcome.Status != domain.RecoveryOutcomeStatusUnknown {
		t.Fatalf("expected outcome UNKNOWN, got %s", recOutcome.Status)
	}
	if recOutcome.RecoveredAmount.MinorUnits != 0 {
		t.Fatalf("expected zero recovered amount, got %d", recOutcome.RecoveredAmount.MinorUnits)
	}
}

func TestReconciliationEngine_NoProviderReferenceResolvesToUnknown(t *testing.T) {
	pool := testPool(t)
	// No ProviderReference at all — nothing could ever be looked up.
	recoveryCase, _ := seedVerifyingCaseWithAction(t, pool, "razorpay", "", domain.RecoveryActionStatusUnknown)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 99999, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusUnknown {
		t.Fatalf("expected UNKNOWN, got %s", outcome.Case.Status)
	}
	if reconciler.InvocationCount() != 0 {
		t.Fatalf("expected zero reconciler invocations with no provider reference, got %d", reconciler.InvocationCount())
	}
}

func TestReconciliationEngine_CaseNotFound(t *testing.T) {
	pool := testPool(t)
	engine := newReconciliationEngine(pool, service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 100, "INR"))
	_, err := engine.Reconcile(context.Background(), uuid.New())
	if !errors.Is(err, service.ErrRecoveryCaseNotFound) {
		t.Fatalf("expected ErrRecoveryCaseNotFound, got %v", err)
	}
}

func TestReconciliationEngine_CaseNotVerifyingRejected(t *testing.T) {
	pool := testPool(t)
	recoveryCase, _ := seedAllowDecision(t, pool) // ALLOW, never executed
	engine := newReconciliationEngine(pool, service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 100, "INR"))

	_, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if !errors.Is(err, service.ErrRecoveryCaseNotVerifying) {
		t.Fatalf("expected ErrRecoveryCaseNotVerifying, got %v", err)
	}
}

func TestReconciliationEngine_NoRecoveryActionForCase(t *testing.T) {
	pool := testPool(t)
	recoveryCase, _ := seedAllowDecision(t, pool)
	now := time.Now().UTC()
	caseRepo := repository.NewPostgresRecoveryCaseRepository(pool)
	// Force the case straight to VERIFYING without ever creating a
	// RecoveryAction, to exercise the structural guard defensively.
	if err := caseRepo.UpdateStatus(context.Background(), recoveryCase.ID, domain.RecoveryCaseStatusAllow, domain.RecoveryCaseStatusExecuting, now); err != nil {
		t.Fatalf("transition to EXECUTING: %v", err)
	}
	if err := caseRepo.UpdateStatus(context.Background(), recoveryCase.ID, domain.RecoveryCaseStatusExecuting, domain.RecoveryCaseStatusVerifying, now); err != nil {
		t.Fatalf("transition to VERIFYING: %v", err)
	}

	engine := newReconciliationEngine(pool, service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 100, "INR"))
	_, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if !errors.Is(err, service.ErrNoRecoveryActionForCase) {
		t.Fatalf("expected ErrNoRecoveryActionForCase, got %v", err)
	}
}

func TestReconciliationEngine_IdempotentAcrossRepeatedCalls(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusUnknown)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 49950, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	first, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if !first.Applied {
		t.Fatal("expected first call to apply the outcome")
	}

	second, err := engine.Reconcile(context.Background(), recoveryCase.ID)
	if err == nil {
		t.Fatalf("expected second Reconcile on an already-resolved case to error (not VERIFYING), got outcome %+v", second)
	}
	if !errors.Is(err, service.ErrRecoveryCaseNotVerifying) {
		t.Fatalf("expected ErrRecoveryCaseNotVerifying, got %v", err)
	}

	assertOutcomeCount(t, pool, recoveryCase.ID, 1)
	_ = action
}

func TestReconciliationEngine_ConcurrentReconciliationConvergesSafely(t *testing.T) {
	pool := testPool(t)
	recoveryCase, _ := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusUnknown)
	reconciler := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 49950, "INR")
	engine := newReconciliationEngine(pool, reconciler)

	const n = 5
	var wg sync.WaitGroup
	applied := make([]bool, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcome, err := engine.Reconcile(context.Background(), recoveryCase.ID)
			errs[i] = err
			if err == nil {
				applied[i] = outcome.Applied
			}
		}(i)
	}
	wg.Wait()

	appliedCount := 0
	for i, err := range errs {
		if err != nil && !errors.Is(err, service.ErrRecoveryCaseNotVerifying) {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
		if err == nil && applied[i] {
			appliedCount++
		}
	}
	if appliedCount != 1 {
		t.Fatalf("expected exactly 1 goroutine to apply the outcome, got %d", appliedCount)
	}

	assertOutcomeCount(t, pool, recoveryCase.ID, 1)
	sumRecoveredAmount(t, pool, recoveryCase.ID, 49950)

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusSuccess {
		t.Fatalf("expected final status SUCCESS, got %s", persistedCase.Status)
	}
}
