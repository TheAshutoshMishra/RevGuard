package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

func TestRecoveryContextBuilder_BuildsCompleteContext(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	now := time.Now().UTC()

	payment := seedPayment(t, pool)

	// Two payment attempts.
	attemptRepo := repository.NewPostgresPaymentAttemptRepository(pool)
	for i := 1; i <= 2; i++ {
		attempt := &domain.PaymentAttempt{
			ID: uuid.New(), PaymentID: payment.ID, AttemptNumber: i,
			Status: domain.PaymentAttemptStatusFailed, FailureCode: "insufficient_funds",
			FailureReason: "not enough balance", StartedAt: now, CreatedAt: now,
		}
		if err := attemptRepo.Create(ctx, attempt); err != nil {
			t.Fatalf("create attempt: %v", err)
		}
	}

	// A recovery case and one prior recovery action.
	amount, _ := domain.NewMoney(49950, "INR")
	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusAnalyzing,
		RevenueAtRisk: amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	actionRepo := repository.NewPostgresRecoveryActionRepository(pool)
	action := &domain.RecoveryAction{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, ActionType: domain.RecoveryActionTypeRetryPayment,
		Status: domain.RecoveryActionStatusFailed, AttemptNumber: 1,
		IdempotencyKey: uuid.New().String(), RequestedAt: now, CreatedAt: now,
	}
	if err := actionRepo.Create(ctx, action); err != nil {
		t.Fatalf("create recovery action: %v", err)
	}

	builder := service.NewRecoveryContextBuilder(
		repository.NewPostgresPaymentRepository(pool), attemptRepo, actionRepo,
	)

	req, err := builder.Build(ctx, recoveryCase, "payment.failed")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if req.CaseID != recoveryCase.ID {
		t.Errorf("CaseID mismatch")
	}
	if req.Context.PaymentID != payment.ID {
		t.Errorf("PaymentID mismatch")
	}
	if req.Context.AmountMinorUnits != 49950 || req.Context.Currency != "INR" {
		t.Errorf("amount/currency mismatch: got %d %s", req.Context.AmountMinorUnits, req.Context.Currency)
	}
	if len(req.Context.PaymentAttempts) != 2 {
		t.Fatalf("expected 2 payment attempts in context, got %d", len(req.Context.PaymentAttempts))
	}
	if req.Context.PaymentAttempts[0].FailureCode != "insufficient_funds" {
		t.Errorf("failure code not carried through: got %q", req.Context.PaymentAttempts[0].FailureCode)
	}
	if len(req.Context.PreviousRecoveryActions) != 1 {
		t.Fatalf("expected 1 prior recovery action in context, got %d", len(req.Context.PreviousRecoveryActions))
	}
	if req.Context.PreviousRecoveryActions[0].ActionType != string(domain.RecoveryActionTypeRetryPayment) {
		t.Errorf("action type not carried through: got %q", req.Context.PreviousRecoveryActions[0].ActionType)
	}
}

// TestRecoveryContextBuilder_ExcludesSensitiveInformation is a static
// safety net: it marshals the assembled context to JSON and asserts none
// of the forbidden substrings appear anywhere in it. AIRecoveryContext's
// field set is fixed and small (see ai_client.go) so this is mostly a
// guard against a future field addition accidentally introducing a
// secret-shaped field.
func TestRecoveryContextBuilder_ExcludesSensitiveInformation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	payment := seedPayment(t, pool)

	amount, _ := domain.NewMoney(49950, "INR")
	now := time.Now().UTC()
	recoveryCase := &domain.RecoveryCase{
		ID: uuid.New(), MerchantID: payment.MerchantID, CustomerID: payment.CustomerID,
		PaymentID: payment.ID, Status: domain.RecoveryCaseStatusAnalyzing,
		RevenueAtRisk: amount, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresRecoveryCaseRepository(pool).Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}

	builder := service.NewRecoveryContextBuilder(
		repository.NewPostgresPaymentRepository(pool),
		repository.NewPostgresPaymentAttemptRepository(pool),
		repository.NewPostgresRecoveryActionRepository(pool),
	)

	req, err := builder.Build(ctx, recoveryCase, "payment.failed")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	lower := strings.ToLower(string(raw))

	forbidden := []string{
		"card_number", "cardnumber", "cvv", "cvc", "api_key", "apikey",
		"password", "secret", "credential", "auth_token", "pan",
	}
	for _, f := range forbidden {
		if strings.Contains(lower, f) {
			t.Errorf("recovery context JSON unexpectedly contains forbidden substring %q:\n%s", f, raw)
		}
	}
}
