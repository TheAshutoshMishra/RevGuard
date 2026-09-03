package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// TestMain requires TEST_DATABASE_URL to point at a disposable PostgreSQL
// database with migrations already applied (see backend/migrations and
// cmd/migrate). Tests are skipped when it is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping repository integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustMoney(t *testing.T, minorUnits int64, currency string) domain.Money {
	t.Helper()
	c, err := domain.NewCurrency(currency)
	if err != nil {
		t.Fatalf("NewCurrency: %v", err)
	}
	m, err := domain.NewMoney(minorUnits, c)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return m
}

// TestRepositoryRoundTrip exercises the full domain graph end-to-end:
// merchant -> customer -> payment -> payment attempt -> recovery case ->
// recovery action -> recovery outcome, plus recovery/audit events. It
// verifies both persistence and the database constraints (FKs, uniqueness)
// established by the Milestone 1 migrations.
func TestRepositoryRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	merchantRepo := repository.NewPostgresMerchantRepository(pool)
	customerRepo := repository.NewPostgresCustomerRepository(pool)
	paymentRepo := repository.NewPostgresPaymentRepository(pool)
	attemptRepo := repository.NewPostgresPaymentAttemptRepository(pool)
	caseRepo := repository.NewPostgresRecoveryCaseRepository(pool)
	actionRepo := repository.NewPostgresRecoveryActionRepository(pool)
	outcomeRepo := repository.NewPostgresRecoveryOutcomeRepository(pool)
	eventRepo := repository.NewPostgresRecoveryEventRepository(pool)
	auditRepo := repository.NewPostgresAuditEventRepository(pool)

	merchant := &domain.Merchant{
		ID:        uuid.New(),
		Name:      "Test Merchant",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := merchantRepo.Create(ctx, merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	got, err := merchantRepo.GetByID(ctx, merchant.ID)
	if err != nil {
		t.Fatalf("get merchant: %v", err)
	}
	if got.Name != merchant.Name {
		t.Fatalf("merchant name mismatch: got %q want %q", got.Name, merchant.Name)
	}

	customer := &domain.Customer{
		ID:                 uuid.New(),
		MerchantID:         merchant.ID,
		ExternalCustomerID: "ext-cust-1",
		Email:              "customer@example.com",
		Name:               "Jane Doe",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := customerRepo.Create(ctx, customer); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	// ₹499.50 is represented as 49950 minor units with currency INR — the
	// Milestone 1 money-handling requirement.
	amount := mustMoney(t, 49950, "INR")
	payment := &domain.Payment{
		ID:                uuid.New(),
		MerchantID:        merchant.ID,
		CustomerID:        customer.ID,
		ExternalPaymentID: "ext-pay-1",
		Amount:            amount,
		Status:            domain.PaymentStatusFailed,
		PaymentMethod:     "card",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := paymentRepo.Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}
	gotPayment, err := paymentRepo.GetByID(ctx, payment.ID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	if gotPayment.Amount.MinorUnits != 49950 || gotPayment.Amount.Currency != "INR" {
		t.Fatalf("payment amount mismatch: got %+v", gotPayment.Amount)
	}

	attempt := &domain.PaymentAttempt{
		ID:            uuid.New(),
		PaymentID:     payment.ID,
		AttemptNumber: 1,
		Status:        domain.PaymentAttemptStatusFailed,
		FailureCode:   "insufficient_funds",
		FailureReason: "The customer's account has insufficient funds",
		StartedAt:     now,
		CreatedAt:     now,
	}
	if err := attemptRepo.Create(ctx, attempt); err != nil {
		t.Fatalf("create payment attempt: %v", err)
	}

	recoveryCase := &domain.RecoveryCase{
		ID:            uuid.New(),
		MerchantID:    merchant.ID,
		CustomerID:    customer.ID,
		PaymentID:     payment.ID,
		Status:        domain.RecoveryCaseStatusDetected,
		RevenueAtRisk: amount,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := caseRepo.Create(ctx, recoveryCase); err != nil {
		t.Fatalf("create recovery case: %v", err)
	}
	gotCase, err := caseRepo.GetByID(ctx, recoveryCase.ID)
	if err != nil {
		t.Fatalf("get recovery case: %v", err)
	}
	if gotCase.Status != domain.RecoveryCaseStatusDetected {
		t.Fatalf("recovery case status mismatch: got %q", gotCase.Status)
	}

	action := &domain.RecoveryAction{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCase.ID,
		ActionType:     domain.RecoveryActionTypeRetryPayment,
		Status:         domain.RecoveryActionStatusSucceeded,
		AttemptNumber:  1,
		IdempotencyKey: "idem-" + recoveryCase.ID.String(),
		RequestedAt:    now,
		CreatedAt:      now,
	}
	if err := actionRepo.Create(ctx, action); err != nil {
		t.Fatalf("create recovery action: %v", err)
	}

	outcome := &domain.RecoveryOutcome{
		ID:                uuid.New(),
		RecoveryCaseID:    recoveryCase.ID,
		RecoveryActionID:  action.ID,
		Status:            domain.RecoveryOutcomeStatusSuccess,
		RecoveredAmount:   amount,
		ExternalReference: "ref-123",
		ObservedAt:        now,
		CreatedAt:         now,
	}
	if err := outcomeRepo.Create(ctx, outcome); err != nil {
		t.Fatalf("create recovery outcome: %v", err)
	}
	gotOutcome, err := outcomeRepo.GetByID(ctx, outcome.ID)
	if err != nil {
		t.Fatalf("get recovery outcome: %v", err)
	}
	if gotOutcome.Status != domain.RecoveryOutcomeStatusSuccess {
		t.Fatalf("recovery outcome status mismatch: got %q", gotOutcome.Status)
	}

	event := &domain.RecoveryEvent{
		ID:            uuid.New(),
		EventID:       "evt-" + payment.ID.String(),
		EventType:     domain.RecoveryEventTypePaymentFailed,
		AggregateType: "payment",
		AggregateID:   payment.ID,
		MerchantID:    merchant.ID,
		Payload:       []byte(`{"reason":"insufficient_funds"}`),
		OccurredAt:    now,
		CreatedAt:     now,
	}
	if err := eventRepo.Create(ctx, event); err != nil {
		t.Fatalf("create recovery event: %v", err)
	}
	gotEvent, err := eventRepo.GetByID(ctx, event.ID)
	if err != nil {
		t.Fatalf("get recovery event: %v", err)
	}
	if gotEvent.EventType != domain.RecoveryEventTypePaymentFailed {
		t.Fatalf("recovery event type mismatch: got %q", gotEvent.EventType)
	}

	audit := &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCase.ID,
		EventType:      "recovery.action.decided",
		ActorType:      domain.AuditActorTypePolicyEngine,
		ActorID:        "policy-v0",
		Metadata:       []byte(`{"decision":"ALLOW"}`),
		CreatedAt:      now,
	}
	if err := auditRepo.Create(ctx, audit); err != nil {
		t.Fatalf("create audit event: %v", err)
	}
	gotAudit, err := auditRepo.GetByID(ctx, audit.ID)
	if err != nil {
		t.Fatalf("get audit event: %v", err)
	}
	if gotAudit.ActorType != domain.AuditActorTypePolicyEngine {
		t.Fatalf("audit event actor type mismatch: got %q", gotAudit.ActorType)
	}
}

// TestForeignKeyConstraintEnforced verifies the database rejects a customer
// referencing a nonexistent merchant.
func TestForeignKeyConstraintEnforced(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	customerRepo := repository.NewPostgresCustomerRepository(pool)
	now := time.Now().UTC()

	err := customerRepo.Create(ctx, &domain.Customer{
		ID:                 uuid.New(),
		MerchantID:         uuid.New(), // does not exist
		ExternalCustomerID: "orphan",
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err == nil {
		t.Fatal("expected foreign key violation, got nil error")
	}
}

// TestGetByIDNotFound verifies repositories return ErrNotFound for a
// nonexistent ID rather than a bare driver error.
func TestGetByIDNotFound(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	merchantRepo := repository.NewPostgresMerchantRepository(pool)
	_, err := merchantRepo.GetByID(ctx, uuid.New())
	if err != repository.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
