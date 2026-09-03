package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

// seedVerifyingCaseWithAction drives a real ALLOW PolicyDecision (via
// seedAllowDecision) to a genuine ALLOW RecoveryCase, then directly
// records the RecoveryAction Milestone 6's ExecutionEngine would have
// produced for the given provider/reference/status and advances the case
// through EXECUTING -> VERIFYING using the same guarded status
// transitions ExecutionEngine itself uses. This is constructed directly
// (rather than via a real PaymentProvider) so tests can exercise every
// provider/reference/status combination Milestone 7 needs to correlate
// against — in particular, a Razorpay-shaped webhook only ever correlates
// to an action recorded with Provider="razorpay", which FakeProvider
// (Provider()=="fake") cannot produce.
func seedVerifyingCaseWithAction(
	t *testing.T, pool *pgxpool.Pool, provider, providerReference string, actionStatus domain.RecoveryActionStatus,
) (*domain.RecoveryCase, *domain.RecoveryAction) {
	t.Helper()
	recoveryCase, decision := seedAllowDecision(t, pool)
	ctx := context.Background()
	now := time.Now().UTC()

	action := &domain.RecoveryAction{
		ID: uuid.New(), RecoveryCaseID: recoveryCase.ID, ActionType: domain.RecoveryActionTypeRetryPayment,
		Status: actionStatus, AttemptNumber: 1,
		IdempotencyKey: "policy-decision:" + decision.ID.String(),
		RequestedAt:    now, ExecutedAt: &now, CreatedAt: now,
		Provider: provider, ProviderReference: providerReference,
	}
	if _, err := repository.NewPostgresRecoveryActionRepository(pool).TryCreate(ctx, action); err != nil {
		t.Fatalf("create recovery action: %v", err)
	}

	caseRepo := repository.NewPostgresRecoveryCaseRepository(pool)
	if err := caseRepo.UpdateStatus(ctx, recoveryCase.ID, domain.RecoveryCaseStatusAllow, domain.RecoveryCaseStatusExecuting, now); err != nil {
		t.Fatalf("transition to EXECUTING: %v", err)
	}
	if err := caseRepo.UpdateStatus(ctx, recoveryCase.ID, domain.RecoveryCaseStatusExecuting, domain.RecoveryCaseStatusVerifying, now); err != nil {
		t.Fatalf("transition to VERIFYING: %v", err)
	}
	recoveryCase.Status = domain.RecoveryCaseStatusVerifying
	return recoveryCase, action
}

func newPlinkReference() string {
	return "plink_" + uuid.NewString()
}

// newEventID returns a fresh, unique provider event id. Tests must never
// reuse a fixed literal across runs: the test database is not truncated
// between test executions, and provider_event_id is globally unique per
// provider, so a hardcoded literal would collide with a leftover row from
// a previous run and be misread as a duplicate delivery.
func newEventID(label string) string {
	return label + "-" + uuid.NewString()
}

const testWebhookSecret = "test-webhook-secret"

func newWebhookProcessor(pool *pgxpool.Pool) *service.WebhookProcessor {
	verifier, err := service.NewRazorpayWebhookVerifier(testWebhookSecret)
	if err != nil {
		panic(err)
	}
	return service.NewWebhookProcessor(pool, verifier, service.NewRazorpayWebhookParser(), nil)
}

func razorpayPaidBody(providerReference string, amountMinorUnits int64, currency string) []byte {
	body := fmt.Sprintf(`{
		"event": "payment_link.paid",
		"payload": {
			"payment_link": {"entity": {"id": %q, "status": "paid"}},
			"payment": {"entity": {"id": "pay_test", "amount": %d, "currency": %q, "status": "captured"}}
		},
		"created_at": 1700000000
	}`, providerReference, amountMinorUnits, currency)
	return []byte(body)
}

func razorpayCancelledBody(providerReference string) []byte {
	return []byte(fmt.Sprintf(`{"event":"payment_link.cancelled","payload":{"payment_link":{"entity":{"id":%q,"status":"cancelled"}}},"created_at":1700000000}`, providerReference))
}

func deliverWebhook(t *testing.T, processor *service.WebhookProcessor, body []byte, eventID string) (*service.WebhookOutcome, error) {
	t.Helper()
	return processor.Process(context.Background(), body, sign(testWebhookSecret, body), eventID)
}

func TestWebhookProcessor_InvalidSignatureRejected(t *testing.T) {
	pool := testPool(t)
	_, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	body := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	_, err := processor.Process(context.Background(), body, "not-a-valid-signature", "evt_bad_sig")
	if !errors.Is(err, service.ErrInvalidWebhookSignature) {
		t.Fatalf("expected ErrInvalidWebhookSignature, got %v", err)
	}

	// Invalid signature must never change financial state.
	persistedCase, gErr := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), action.RecoveryCaseID)
	if gErr != nil {
		t.Fatalf("GetByID: %v", gErr)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case to remain VERIFYING after rejected signature, got %s", persistedCase.Status)
	}
	assertOutcomeCount(t, pool, action.RecoveryCaseID, 0)
}

func TestWebhookProcessor_MalformedPayloadRejected(t *testing.T) {
	pool := testPool(t)
	processor := newWebhookProcessor(pool)
	body := []byte(`{not valid json`)
	_, err := processor.Process(context.Background(), body, sign(testWebhookSecret, body), "evt_malformed")
	if !errors.Is(err, service.ErrMalformedWebhookPayload) {
		t.Fatalf("expected ErrMalformedWebhookPayload, got %v", err)
	}
}

func TestWebhookProcessor_MissingRequiredFields(t *testing.T) {
	pool := testPool(t)
	processor := newWebhookProcessor(pool)
	body := []byte(`{"payload":{}}`) // no "event" field
	_, err := processor.Process(context.Background(), body, sign(testWebhookSecret, body), "evt_missing_fields")
	if !errors.Is(err, service.ErrMalformedWebhookPayload) {
		t.Fatalf("expected ErrMalformedWebhookPayload, got %v", err)
	}
}

func TestWebhookProcessor_CapturedEstablishesSuccess(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	body := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	outcome, err := deliverWebhook(t, processor, body, newEventID("evt_captured_1"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !outcome.FinancialOutcomeApplied {
		t.Fatal("expected financial outcome to be applied")
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
	if recOutcome.Source != domain.RecoveryOutcomeSourceWebhook {
		t.Fatalf("expected source WEBHOOK, got %s", recOutcome.Source)
	}
	if recOutcome.ExternalReference != action.ProviderReference {
		t.Fatalf("expected external reference %q, got %q", action.ProviderReference, recOutcome.ExternalReference)
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusSuccess {
		t.Fatalf("persisted case status: got %s, want SUCCESS", persistedCase.Status)
	}
	assertOutcomeCount(t, pool, recoveryCase.ID, 1)

	recordedCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_outcome.recorded'`, recoveryCase.ID)
	if recordedCount != 1 {
		t.Fatalf("expected 1 recovery_outcome.recorded audit row, got %d", recordedCount)
	}
}

func TestWebhookProcessor_CancelledEstablishesFailed(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	body := razorpayCancelledBody(action.ProviderReference)
	outcome, err := deliverWebhook(t, processor, body, newEventID("evt_cancelled_1"))
	if err != nil {
		t.Fatalf("Process: %v", err)
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
		t.Fatalf("expected zero recovered amount for FAILED, got %d", recOutcome.RecoveredAmount.MinorUnits)
	}
	_ = recoveryCase
}

func TestWebhookProcessor_UnknownProviderEventTypeIsPending(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	body := []byte(fmt.Sprintf(`{"event":"payment_link.partially_paid","payload":{"payment_link":{"entity":{"id":%q}}}}`, action.ProviderReference))
	outcome, err := deliverWebhook(t, processor, body, newEventID("evt_pending_1"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if outcome.FinancialOutcomeApplied {
		t.Fatal("expected no financial outcome for a PENDING observation")
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case to remain VERIFYING, got %s", persistedCase.Status)
	}
	assertOutcomeCount(t, pool, recoveryCase.ID, 0)
}

func TestWebhookProcessor_ValidWebhookForUnknownResource(t *testing.T) {
	pool := testPool(t)
	processor := newWebhookProcessor(pool)

	body := razorpayPaidBody("plink_does_not_exist_"+uuid.NewString(), 100, "INR")
	outcome, err := deliverWebhook(t, processor, body, newEventID("evt_unmatched_1"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if outcome.Event.Matched {
		t.Fatal("expected event to be unmatched")
	}
	if outcome.FinancialOutcomeApplied {
		t.Fatal("expected no financial outcome for an unmatched event")
	}
	if outcome.Case != nil {
		t.Fatal("expected no case for an unmatched event")
	}
}

func TestWebhookProcessor_DuplicateDeliveryIsIdempotent(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	eventID := newEventID("evt_dup")
	body := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	first, err := deliverWebhook(t, processor, body, eventID)
	if err != nil {
		t.Fatalf("first Process: %v", err)
	}
	if first.Duplicate {
		t.Fatal("first delivery should not be a duplicate")
	}

	second, err := deliverWebhook(t, processor, body, eventID)
	if err != nil {
		t.Fatalf("second Process: %v", err)
	}
	if !second.Duplicate {
		t.Fatal("second identical delivery should be reported as a duplicate")
	}

	assertOutcomeCount(t, pool, recoveryCase.ID, 1)
	sumRecoveredAmount(t, pool, recoveryCase.ID, 49950)

	events := countRows(t, pool,
		`SELECT count(*) FROM provider_webhook_events WHERE provider = 'razorpay' AND provider_event_id = $1`, eventID)
	if events != 1 {
		t.Fatalf("expected exactly 1 provider_webhook_events row, got %d", events)
	}
}

func TestWebhookProcessor_WebhookAfterAlreadySuccessIsNoOp(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	first := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	if _, err := deliverWebhook(t, processor, first, newEventID("evt_after_success_1")); err != nil {
		t.Fatalf("first Process: %v", err)
	}

	// A second, DIFFERENT event (different provider_event_id, same
	// resolved payment) arriving after the case is already SUCCESS must
	// never double-apply a financial outcome.
	second := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	outcome, err := deliverWebhook(t, processor, second, newEventID("evt_after_success_2"))
	if err != nil {
		t.Fatalf("second Process: %v", err)
	}
	if outcome.FinancialOutcomeApplied {
		t.Fatal("expected no financial outcome once the case is already resolved")
	}

	assertOutcomeCount(t, pool, recoveryCase.ID, 1)
	sumRecoveredAmount(t, pool, recoveryCase.ID, 49950)

	rejectedCount := countRows(t, pool,
		`SELECT count(*) FROM audit_events WHERE recovery_case_id = $1 AND event_type = 'recovery_outcome.rejected'`, recoveryCase.ID)
	if rejectedCount != 1 {
		t.Fatalf("expected 1 recovery_outcome.rejected audit row, got %d", rejectedCount)
	}
}

func TestWebhookProcessor_WebhookAfterAlreadyFailedIsNoOp(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	if _, err := deliverWebhook(t, processor, razorpayCancelledBody(action.ProviderReference), newEventID("evt_fail_1")); err != nil {
		t.Fatalf("first Process: %v", err)
	}

	// A later CAPTURED observation for the same reference must not
	// overturn an already-established FAILED outcome.
	outcome, err := deliverWebhook(t, processor, razorpayPaidBody(action.ProviderReference, 49950, "INR"), newEventID("evt_fail_2"))
	if err != nil {
		t.Fatalf("second Process: %v", err)
	}
	if outcome.FinancialOutcomeApplied {
		t.Fatal("expected no financial outcome once the case is already resolved to FAILED")
	}

	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusFailed {
		t.Fatalf("expected case to remain FAILED, got %s", persistedCase.Status)
	}
	assertOutcomeCount(t, pool, recoveryCase.ID, 1)
}

func TestWebhookProcessor_ConcurrentDuplicateDeliveryNoDoubleCount(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)
	body := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	eventID := newEventID("evt_concurrent")

	const n = 5
	var wg sync.WaitGroup
	applied := make([]bool, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcome, err := deliverWebhook(t, processor, body, eventID)
			errs[i] = err
			if err == nil {
				applied[i] = outcome.FinancialOutcomeApplied
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	appliedCount := 0
	for _, a := range applied {
		if a {
			appliedCount++
		}
	}
	if appliedCount != 1 {
		t.Fatalf("expected exactly 1 goroutine to apply the financial outcome, got %d", appliedCount)
	}

	assertOutcomeCount(t, pool, recoveryCase.ID, 1)
	sumRecoveredAmount(t, pool, recoveryCase.ID, 49950)

	events := countRows(t, pool,
		`SELECT count(*) FROM provider_webhook_events WHERE provider = 'razorpay' AND provider_event_id = $1`, eventID)
	if events != 1 {
		t.Fatalf("expected exactly 1 provider_webhook_events row despite concurrent delivery, got %d", events)
	}
}

func TestWebhookProcessor_CapturedMissingAmountNotGuessed(t *testing.T) {
	pool := testPool(t)
	recoveryCase, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)

	// A "paid" event with no payment sub-object at all — no definitive
	// amount/currency to establish SUCCESS.
	body := []byte(fmt.Sprintf(`{"event":"payment_link.paid","payload":{"payment_link":{"entity":{"id":%q,"status":"paid"}}}}`, action.ProviderReference))
	outcome, err := deliverWebhook(t, processor, body, newEventID("evt_missing_amount_1"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if outcome.FinancialOutcomeApplied {
		t.Fatal("expected no fabricated SUCCESS without a definitive amount")
	}
	persistedCase, err := repository.NewPostgresRecoveryCaseRepository(pool).GetByID(context.Background(), recoveryCase.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persistedCase.Status != domain.RecoveryCaseStatusVerifying {
		t.Fatalf("expected case to remain VERIFYING, got %s", persistedCase.Status)
	}
	assertOutcomeCount(t, pool, recoveryCase.ID, 0)
}

func TestWebhookProcessor_NoSecretsInPersistedMetadata(t *testing.T) {
	pool := testPool(t)
	_, action := seedVerifyingCaseWithAction(t, pool, "razorpay", newPlinkReference(), domain.RecoveryActionStatusSucceeded)
	processor := newWebhookProcessor(pool)
	body := razorpayPaidBody(action.ProviderReference, 49950, "INR")
	eventID := newEventID("evt_secret_check")
	if _, err := deliverWebhook(t, processor, body, eventID); err != nil {
		t.Fatalf("Process: %v", err)
	}

	forbidden := []string{"card_number", "cvv", "api_key", "secret", "password", "authorization", testWebhookSecret}
	rows, err := pool.Query(context.Background(),
		`SELECT metadata FROM provider_webhook_events WHERE provider = 'razorpay' AND provider_event_id = $1`, eventID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var asMap map[string]any
		_ = json.Unmarshal(raw, &asMap)
		text := string(raw)
		for _, f := range forbidden {
			if containsFold(text, f) {
				t.Fatalf("forbidden substring %q found in provider_webhook_events.metadata: %s", f, text)
			}
		}
	}
}

func assertOutcomeCount(t *testing.T, pool *pgxpool.Pool, recoveryCaseID uuid.UUID, want int) {
	t.Helper()
	got := countRows(t, pool, `SELECT count(*) FROM recovery_outcomes WHERE recovery_case_id = $1`, recoveryCaseID)
	if got != want {
		t.Fatalf("expected %d recovery_outcomes rows, got %d", want, got)
	}
}

func sumRecoveredAmount(t *testing.T, pool *pgxpool.Pool, recoveryCaseID uuid.UUID, want int64) {
	t.Helper()
	var sum int64
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(recovered_amount_minor_units), 0) FROM recovery_outcomes WHERE recovery_case_id = $1`, recoveryCaseID).Scan(&sum)
	if err != nil {
		t.Fatalf("sum query: %v", err)
	}
	if sum != want {
		t.Fatalf("expected total recovered amount %d, got %d (double-counting?)", want, sum)
	}
}
