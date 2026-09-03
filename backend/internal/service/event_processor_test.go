package service_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

// testPool requires TEST_DATABASE_URL to point at a disposable PostgreSQL
// database with migrations already applied (see backend/migrations and
// cmd/migrate). Tests are skipped when it is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping event processor integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedPayment creates a merchant, customer, and payment directly through
// the Milestone 1 repositories so tests have a real aggregate to
// correlate events against.
func seedPayment(t *testing.T, pool *pgxpool.Pool) *domain.Payment {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	merchant := &domain.Merchant{ID: uuid.New(), Name: "Test Merchant", CreatedAt: now, UpdatedAt: now}
	if err := repository.NewPostgresMerchantRepository(pool).Create(ctx, merchant); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}

	customer := &domain.Customer{
		ID: uuid.New(), MerchantID: merchant.ID, ExternalCustomerID: uuid.New().String(),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresCustomerRepository(pool).Create(ctx, customer); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	amount, err := domain.NewMoney(49950, "INR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	payment := &domain.Payment{
		ID: uuid.New(), MerchantID: merchant.ID, CustomerID: customer.ID,
		ExternalPaymentID: uuid.New().String(), Amount: amount,
		Status: domain.PaymentStatusFailed, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresPaymentRepository(pool).Create(ctx, payment); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	return payment
}

func eventInputFor(payment *domain.Payment, eventType string) service.EventInput {
	return service.EventInput{
		EventID:       uuid.New().String(),
		EventType:     eventType,
		AggregateType: "payment",
		AggregateID:   payment.ID.String(),
		MerchantID:    payment.MerchantID.String(),
		OccurredAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:       []byte(`{"reason":"insufficient_funds"}`),
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return n
}

func TestEventProcessor_Idempotency(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	input := eventInputFor(payment, "payment.failed")

	first, err := processor.Process(context.Background(), input)
	if err != nil {
		t.Fatalf("first Process: %v", err)
	}
	if first.Duplicate {
		t.Fatal("first call should not be a duplicate")
	}
	if first.RecoveryCase == nil {
		t.Fatal("expected a recovery case to be created")
	}

	second, err := processor.Process(context.Background(), input)
	if err != nil {
		t.Fatalf("second Process: %v", err)
	}
	if !second.Duplicate {
		t.Fatal("second call with the same event_id should be a duplicate")
	}

	eventCount := countRows(t, pool, `SELECT count(*) FROM recovery_events WHERE event_id = $1`, input.EventID)
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 recovery_events row, got %d", eventCount)
	}
	caseCount := countRows(t, pool, `SELECT count(*) FROM recovery_cases WHERE payment_id = $1`, payment.ID)
	if caseCount != 1 {
		t.Fatalf("expected exactly 1 recovery_cases row, got %d", caseCount)
	}
}

func TestEventProcessor_QualifyingEventTypesCreateCases(t *testing.T) {
	pool := testPool(t)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	qualifying := []string{
		"payment.failed", "checkout.abandoned", "subscription.failed",
		"mandate.failed", "invoice.overdue",
	}
	for _, eventType := range qualifying {
		t.Run(eventType, func(t *testing.T) {
			payment := seedPayment(t, pool)
			result, err := processor.Process(context.Background(), eventInputFor(payment, eventType))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if result.RecoveryCase == nil {
				t.Fatal("expected a recovery case to be created")
			}
			if result.RecoveryCase.Status != domain.RecoveryCaseStatusAnalyzing {
				t.Fatalf("expected case status ANALYZING, got %s", result.RecoveryCase.Status)
			}
		})
	}
}

func TestEventProcessor_NonQualifyingEventDoesNotCreateCase(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	result, err := processor.Process(context.Background(), eventInputFor(payment, "payment.succeeded"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.RecoveryCase != nil {
		t.Fatal("payment.succeeded should not create a recovery case")
	}

	caseCount := countRows(t, pool, `SELECT count(*) FROM recovery_cases WHERE payment_id = $1`, payment.ID)
	if caseCount != 0 {
		t.Fatalf("expected 0 recovery_cases rows, got %d", caseCount)
	}
}

func TestEventProcessor_UnsupportedAggregateTypeRejected(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	input := eventInputFor(payment, "payment.failed")
	input.AggregateType = "subscription"

	_, err := processor.Process(context.Background(), input)
	if !errors.Is(err, service.ErrUnsupportedAggregate) {
		t.Fatalf("expected ErrUnsupportedAggregate, got %v", err)
	}
}

func TestEventProcessor_AuditTrailRecordsCreationAndTransition(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	result, err := processor.Process(context.Background(), eventInputFor(payment, "payment.failed"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	created := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_case.created'`,
		result.RecoveryCase.ID)
	if created != 1 {
		t.Fatalf("expected 1 recovery_case.created audit row, got %d", created)
	}

	transitioned := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_case.transitioned'`,
		result.RecoveryCase.ID)
	if transitioned != 1 {
		t.Fatalf("expected 1 recovery_case.transitioned audit row, got %d", transitioned)
	}
}

func TestEventProcessor_ConcurrentDuplicateEventsCreateOnlyOneCase(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	input := eventInputFor(payment, "payment.failed")

	const workers = 5
	results := make([]*service.ProcessResult, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = processor.Process(context.Background(), input)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: Process returned error: %v", i, err)
		}
	}

	caseCount := countRows(t, pool, `SELECT count(*) FROM recovery_cases WHERE payment_id = $1`, payment.ID)
	if caseCount != 1 {
		t.Fatalf("expected exactly 1 recovery_cases row after concurrent duplicate processing, got %d", caseCount)
	}

	created := 0
	for _, r := range results {
		if r.CaseCreated {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent calls to report CaseCreated, got %d", workers, created)
	}
}

// TestEventProcessor_ConcurrentDistinctEventsSamePaymentCreateOnlyOneCase
// exercises the race that migration 000011's partial unique index exists
// to prevent: two different qualifying events for the same payment
// arriving concurrently (e.g. payment.failed and mandate.failed both
// describing the same underlying risk) must not create two open
// RecoveryCases. Unlike the identical-event_id test above, every worker
// here gets past the recovery_events dedup check and reaches
// RecoveryOrchestrator.HandleQualifyingEvent, so this is what actually
// forces the repository.IsUniqueViolation race-recovery path in
// recovery_orchestrator.go to run.
func TestEventProcessor_ConcurrentDistinctEventsSamePaymentCreateOnlyOneCase(t *testing.T) {
	pool := testPool(t)
	payment := seedPayment(t, pool)
	processor := service.NewEventProcessor(pool, service.NewLoggingEventPublisher(nil), nil)

	const workers = 5
	results := make([]*service.ProcessResult, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct event_id per worker, same payment aggregate.
			results[i], errs[i] = processor.Process(context.Background(), eventInputFor(payment, "payment.failed"))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: Process returned error: %v", i, err)
		}
	}

	caseCount := countRows(t, pool, `SELECT count(*) FROM recovery_cases WHERE payment_id = $1`, payment.ID)
	if caseCount != 1 {
		t.Fatalf("expected exactly 1 recovery_cases row after concurrent distinct-event processing, got %d", caseCount)
	}

	created := 0
	sameCaseID := results[0].RecoveryCase.ID
	for _, r := range results {
		if r.RecoveryCase == nil {
			t.Fatal("expected every worker to be linked to a recovery case")
		}
		if r.RecoveryCase.ID != sameCaseID {
			t.Fatalf("expected all workers to converge on the same case, got %s and %s", sameCaseID, r.RecoveryCase.ID)
		}
		if r.CaseCreated {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent calls to report CaseCreated, got %d", workers, created)
	}
}
