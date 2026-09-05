// Command demo drives RevGuard's real, unmodified HTTP API through four
// end-to-end scenarios (Milestone 11), so the complete
// DETECTED -> ... -> SUCCESS/FAILED/UNKNOWN lifecycle can be observed on
// the dashboard from a clean local environment, without any real
// financial transaction.
//
// It requires a running backend (`go run ./cmd/server`) and a running AI
// service (`AI_PROVIDER=mock uvicorn app.main:app`, from ai-service/) —
// see docs/architecture/dashboard.md's "Local startup" section for exact
// commands. It connects directly to the same PostgreSQL database only to
// seed the synthetic merchant/customer/payment fixtures each scenario
// needs (mirroring every prior milestone's manual psql-based smoke
// tests, now automated) and, for Scenario D only, to construct one
// temporary in-process ExecutionEngine with a different FakeProvider
// scenario — see runScenarioD's doc comment for exactly why and how that
// stays safe.
//
// Every scenario uses FakeProvider (never a real Razorpay call), a
// locally-computed webhook signature (never a production secret), and
// creates no state outside PostgreSQL rows scoped to synthetic
// merchants/customers this command creates itself.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/config"
	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
	"revguard/backend/internal/service"
)

func main() {
	apiURL := flag.String("api", "http://localhost:8080", "base URL of a running RevGuard backend (go run ./cmd/server)")
	scenario := flag.String("scenario", "all", "which scenario to run: a, b, c, d, or all")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN())
	if err != nil {
		log.Fatalf("demo: failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	client := &demoClient{httpClient: &http.Client{Timeout: 25 * time.Second}, baseURL: *apiURL}

	if resp, err := http.Get(*apiURL + "/health"); err != nil || resp.StatusCode != http.StatusOK {
		log.Fatalf("demo: backend at %s is not reachable — start it first with `go run ./cmd/server` (see docs/architecture/dashboard.md). error: %v", *apiURL, err)
	}

	run := func(name string, fn func() error) {
		fmt.Printf("\n=== Scenario %s ===\n", name)
		if err := fn(); err != nil {
			log.Fatalf("demo: scenario %s failed: %v", name, err)
		}
	}

	switch *scenario {
	case "a":
		run("A — Successful Recovery", func() error { return runScenarioA(ctx, pool, client, cfg) })
	case "b":
		run("B — Policy Block", func() error { return runScenarioB(ctx, pool, client) })
	case "c":
		run("C — Human Escalation", func() error { return runScenarioC(ctx, pool, client) })
	case "d":
		run("D — Ambiguous Provider Outcome", func() error { return runScenarioD(ctx, pool, client, cfg) })
	case "all":
		run("A — Successful Recovery", func() error { return runScenarioA(ctx, pool, client, cfg) })
		run("B — Policy Block", func() error { return runScenarioB(ctx, pool, client) })
		run("C — Human Escalation", func() error { return runScenarioC(ctx, pool, client) })
		run("D — Ambiguous Provider Outcome", func() error { return runScenarioD(ctx, pool, client, cfg) })
	default:
		log.Fatalf("demo: unknown --scenario %q (expected a, b, c, d, or all)", *scenario)
	}

	fmt.Println("\nDemo complete. Open the dashboard's Recovery Cases page to inspect every case created above.")
}

// seedFixture creates a synthetic merchant/customer/payment (and
// optionally prior payment attempts) directly through the Milestone 1
// repositories — exactly what a real payment.failed event presupposes
// already exists, mirroring every prior milestone's manual psql seeding.
func seedFixture(ctx context.Context, pool *pgxpool.Pool, amountMinorUnits int64, priorFailedAttempts int) (*domain.Payment, error) {
	now := time.Now().UTC()

	merchant := &domain.Merchant{ID: uuid.New(), Name: "Demo Merchant", CreatedAt: now, UpdatedAt: now}
	if err := repository.NewPostgresMerchantRepository(pool).Create(ctx, merchant); err != nil {
		return nil, fmt.Errorf("create merchant: %w", err)
	}
	customer := &domain.Customer{
		ID: uuid.New(), MerchantID: merchant.ID, ExternalCustomerID: "cust_demo_" + uuid.New().String()[:8],
		Email: "demo-customer@example.com", CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresCustomerRepository(pool).Create(ctx, customer); err != nil {
		return nil, fmt.Errorf("create customer: %w", err)
	}
	payment := &domain.Payment{
		ID: uuid.New(), MerchantID: merchant.ID, CustomerID: customer.ID,
		ExternalPaymentID: "pay_demo_" + uuid.New().String()[:8], Status: domain.PaymentStatusFailed,
		Amount: domain.Money{MinorUnits: amountMinorUnits, Currency: "INR"}, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.NewPostgresPaymentRepository(pool).Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("create payment: %w", err)
	}

	attemptRepo := repository.NewPostgresPaymentAttemptRepository(pool)
	for i := 0; i < priorFailedAttempts; i++ {
		if err := attemptRepo.Create(ctx, &domain.PaymentAttempt{
			ID: uuid.New(), PaymentID: payment.ID, AttemptNumber: i + 1,
			Status: domain.PaymentAttemptStatusFailed, FailureCode: "GENERIC_DECLINE",
			FailureReason: "demo: simulated decline", StartedAt: now, CreatedAt: now,
		}); err != nil {
			return nil, fmt.Errorf("create payment attempt: %w", err)
		}
	}
	return payment, nil
}

// runScenarioA: payment.failed -> AI diagnosis -> economics -> policy
// ALLOW -> retry_payment execution -> VERIFYING -> financial truth
// established -> SUCCESS.
//
// Financial truth here comes from RECONCILIATION, not a webhook —
// deliberately. A real finding from building this demo: RevGuard's
// webhook correlation (WebhookProcessor) looks up a RecoveryAction by
// (provider, provider_reference), and the Razorpay webhook parser always
// reports provider="razorpay" (it's the provider-specific
// /v1/webhooks/razorpay endpoint). An action executed by FakeProvider is
// tagged provider="fake". Those can never match — correctly: this
// prevents a locally-simulated action from ever being mistaken for one a
// real Razorpay webhook is describing, and vice versa. Reproducing a
// "real Razorpay webhook" for a FakeProvider-executed action would
// require either weakening that correlation (a safety regression) or
// real Razorpay credentials and a live webhook (forbidden for this
// demo). So this scenario demonstrates the webhook path's correlation
// safety honestly (it reports "not matched", not a fabricated match) and
// then uses reconciliation — equally authoritative per Milestone 7 — to
// actually establish SUCCESS.
func runScenarioA(ctx context.Context, pool *pgxpool.Pool, client *demoClient, cfg config.Config) error {
	payment, err := seedFixture(ctx, pool, 25_000, 1) // small amount, 1 prior attempt: clears every ALLOW rule under the balanced profile
	if err != nil {
		return err
	}

	eventResp, err := client.postEvent(payment, "GENERIC_DECLINE")
	if err != nil {
		return err
	}
	fmt.Printf("event ingested: case=%s status=%s\n", eventResp.RecoveryCaseID, eventResp.CaseStatus)
	if eventResp.CaseStatus != string(domain.RecoveryCaseStatusAllow) {
		return fmt.Errorf("expected ALLOW, got %s (adjust demo fixture if AI/policy defaults changed)", eventResp.CaseStatus)
	}

	execResp, err := client.postExecute(eventResp.RecoveryCaseID)
	if err != nil {
		return err
	}
	fmt.Printf("executed: action=%s status=%s case_status=%s provider_reference=%s\n",
		execResp.RecoveryActionID, execResp.ExecutionStatus, execResp.CaseStatus, execResp.ProviderReference)
	if execResp.CaseStatus != string(domain.RecoveryCaseStatusVerifying) {
		return fmt.Errorf("expected VERIFYING after execution, got %s", execResp.CaseStatus)
	}

	if cfg.RazorpayWebhookSecret != "" {
		body := fmt.Sprintf(`{"event":"payment_link.paid","payload":{"payment_link":{"entity":{"id":%q,"status":"paid"}},"payment":{"entity":{"id":"pay_demo","amount":%d,"currency":"INR","status":"captured"}}},"created_at":%d}`,
			execResp.ProviderReference, payment.Amount.MinorUnits, time.Now().Unix())
		matched, err := client.postSignedWebhook(cfg.RazorpayWebhookSecret, body)
		if err != nil {
			return err
		}
		fmt.Printf("webhook delivered: matched=%v (expected false — see this function's doc comment for why a FakeProvider action never matches a Razorpay-branded webhook, by design)\n", matched)
	} else {
		fmt.Println("RAZORPAY_WEBHOOK_SECRET not set on the backend — skipping the webhook-delivery demonstration (optional).")
	}

	caseStatus, applied, err := client.postReconcile(eventResp.RecoveryCaseID)
	if err != nil {
		return err
	}
	fmt.Printf("reconcile: applied=%v case_status=%s\n", applied, caseStatus)
	if caseStatus != string(domain.RecoveryCaseStatusSuccess) {
		return fmt.Errorf("expected SUCCESS after reconciliation, got %s (start the backend with RECONCILER_FAKE_SCENARIO=payment_captured and RECONCILER_FAKE_AMOUNT_MINOR_UNITS=%d to see this scenario reach SUCCESS — the default reconciler is deliberately inert)", caseStatus, payment.Amount.MinorUnits)
	}
	fmt.Println("Revenue recovered: full amount confirmed by provider reconciliation, not merely 'execution succeeded'.")
	return nil
}

// runScenarioB: payment.failed with 3 prior failed attempts already on
// the underlying payment -> PolicyEngine's rule (F) (MaxPaymentAttempts)
// fires regardless of what the AI recommends -> BLOCK, zero provider
// invocations.
func runScenarioB(ctx context.Context, pool *pgxpool.Pool, client *demoClient) error {
	payment, err := seedFixture(ctx, pool, 25_000, 3) // 3 prior attempts hits the balanced profile's MaxPaymentAttempts
	if err != nil {
		return err
	}
	eventResp, err := client.postEvent(payment, "GENERIC_DECLINE")
	if err != nil {
		return err
	}
	fmt.Printf("event ingested: case=%s status=%s\n", eventResp.RecoveryCaseID, eventResp.CaseStatus)
	if eventResp.CaseStatus != string(domain.RecoveryCaseStatusBlock) {
		return fmt.Errorf("expected BLOCK, got %s", eventResp.CaseStatus)
	}

	if _, err := client.postExecuteExpectingRejection(eventResp.RecoveryCaseID); err != nil {
		return err
	}
	fmt.Println("confirmed: POST /execute rejects a BLOCK decision, zero provider invocations.")
	return nil
}

// runScenarioC: payment.failed with a large amount -> PolicyEngine's
// rule (E) (amount above the auto-approval ceiling) fires ->
// ESCALATE, zero provider invocations.
func runScenarioC(ctx context.Context, pool *pgxpool.Pool, client *demoClient) error {
	payment, err := seedFixture(ctx, pool, 500_000, 1) // above the balanced profile's 100,000 auto-approval ceiling
	if err != nil {
		return err
	}
	eventResp, err := client.postEvent(payment, "GENERIC_DECLINE")
	if err != nil {
		return err
	}
	fmt.Printf("event ingested: case=%s status=%s\n", eventResp.RecoveryCaseID, eventResp.CaseStatus)
	if eventResp.CaseStatus != string(domain.RecoveryCaseStatusEscalate) {
		return fmt.Errorf("expected ESCALATE, got %s", eventResp.CaseStatus)
	}

	if _, err := client.postExecuteExpectingRejection(eventResp.RecoveryCaseID); err != nil {
		return err
	}
	fmt.Println("confirmed: POST /execute rejects an ESCALATE decision — no automatic execution ever occurs for ESCALATE.")
	return nil
}

// runScenarioD demonstrates the ambiguous-provider-outcome path
// (UNKNOWN). The running server's PaymentProvider is fixed at process
// startup (PAYMENT_PROVIDER env var) — there is no per-request way to
// ask it to simulate a timeout for just this one case, and there
// shouldn't be (that per-request flexibility would be exactly the kind
// of client-controlled behavior ExecutionEngine is designed never to
// allow). So, for this scenario only, this demo drives the case to
// ALLOW via the real HTTP API exactly like the others, then calls
// ExecutionEngine.Execute directly, in-process, against the same
// database, with a temporary FakeProvider(timeout) — the exact same
// function the running server calls, with a different (still fake,
// still safe) provider argument. This is the same pattern Milestones
// 4-6 used for their own one-off verification tools
// (cmd/idemcheck/execcheck, deleted after use); this one is kept
// permanently because it is a real, reusable demo scenario.
func runScenarioD(ctx context.Context, pool *pgxpool.Pool, client *demoClient, cfg config.Config) error {
	payment, err := seedFixture(ctx, pool, 25_000, 1)
	if err != nil {
		return err
	}
	eventResp, err := client.postEvent(payment, "GENERIC_DECLINE")
	if err != nil {
		return err
	}
	fmt.Printf("event ingested: case=%s status=%s\n", eventResp.RecoveryCaseID, eventResp.CaseStatus)
	if eventResp.CaseStatus != string(domain.RecoveryCaseStatusAllow) {
		return fmt.Errorf("expected ALLOW, got %s", eventResp.CaseStatus)
	}

	caseID, err := uuid.Parse(eventResp.RecoveryCaseID)
	if err != nil {
		return err
	}
	decision, err := repository.NewPostgresPolicyDecisionRepository(pool).GetLatestByRecoveryCaseID(ctx, caseID)
	if err != nil {
		return fmt.Errorf("load policy decision: %w", err)
	}

	timeoutProvider := service.NewFakeProvider(service.FakeProviderScenarioTimeout)
	engine := service.NewExecutionEngine(pool, timeoutProvider, nil)
	outcome, err := engine.Execute(ctx, caseID, decision.ID)
	if err != nil {
		return fmt.Errorf("execute with timeout provider: %w", err)
	}
	fmt.Printf("executed with a simulated provider timeout: action_status=%s case_status=%s\n", outcome.Action.Status, outcome.Case.Status)
	if outcome.Action.Status != domain.RecoveryActionStatusUnknown {
		return fmt.Errorf("expected action status UNKNOWN, got %s", outcome.Action.Status)
	}
	if outcome.Case.Status != domain.RecoveryCaseStatusVerifying {
		return fmt.Errorf("expected case status VERIFYING (never SUCCESS/FAILED for an ambiguous outcome), got %s", outcome.Case.Status)
	}

	fmt.Println("attempting reconciliation...")
	caseStatus, applied, err := client.postReconcile(eventResp.RecoveryCaseID)
	if err != nil {
		return err
	}
	fmt.Printf("reconcile: applied=%v case_status=%s\n", applied, caseStatus)
	switch {
	case applied && caseStatus == string(domain.RecoveryCaseStatusUnknown):
		fmt.Println("resolved to UNKNOWN: a timed-out execution never received a provider reference, so there is nothing any reconciliation attempt could ever look up — ReconciliationEngine resolves this dead end once, terminal for automation, rather than leaving the case in VERIFYING forever or guessing a result.")
	case applied:
		fmt.Println("reconciliation resolved the case using the server's currently configured reconciler scenario.")
	default:
		fmt.Println("reconciliation did not resolve it — the case remains VERIFYING, exactly as Milestone 7 requires: never fabricate a resolution.")
	}
	return nil
}

// --- HTTP client -------------------------------------------------------

type demoClient struct {
	httpClient *http.Client
	baseURL    string
}

type eventResponse struct {
	RecoveryCaseID string `json:"recovery_case_id"`
	CaseStatus     string `json:"case_status"`
}

type executionResponse struct {
	RecoveryActionID  string `json:"recovery_action_id"`
	ExecutionStatus   string `json:"execution_status"`
	CaseStatus        string `json:"case_status"`
	ProviderReference string `json:"provider_reference"`
}

func (c *demoClient) postEvent(payment *domain.Payment, failureCode string) (*eventResponse, error) {
	payload, _ := json.Marshal(map[string]any{"failure_code": failureCode})
	body, _ := json.Marshal(map[string]any{
		"event_id":       "evt_demo_" + uuid.New().String(),
		"event_type":     "payment.failed",
		"aggregate_type": "payment",
		"aggregate_id":   payment.ID.String(),
		"merchant_id":    payment.MerchantID.String(),
		"occurred_at":    time.Now().UTC().Format(time.RFC3339),
		"payload":        json.RawMessage(payload),
	})

	resp, err := c.httpClient.Post(c.baseURL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("POST /events: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /events: unexpected status %d: %s", resp.StatusCode, raw)
	}
	var out eventResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode /events response: %w", err)
	}
	return &out, nil
}

func (c *demoClient) postExecute(caseID string) (*executionResponse, error) {
	resp, err := c.httpClient.Post(c.baseURL+"/v1/recovery-cases/"+caseID+"/execute", "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("POST /execute: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("POST /execute: unexpected status %d: %s", resp.StatusCode, raw)
	}
	var out executionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode /execute response: %w", err)
	}
	return &out, nil
}

// postExecuteExpectingRejection confirms /execute correctly rejects a
// non-ALLOW case (BLOCK/ESCALATE) rather than executing anything.
func (c *demoClient) postExecuteExpectingRejection(caseID string) (int, error) {
	resp, err := c.httpClient.Post(c.baseURL+"/v1/recovery-cases/"+caseID+"/execute", "application/json", nil)
	if err != nil {
		return 0, fmt.Errorf("POST /execute: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return 0, fmt.Errorf("expected /execute to reject a non-ALLOW case, but it returned %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (c *demoClient) postReconcile(caseID string) (string, bool, error) {
	resp, err := c.httpClient.Post(c.baseURL+"/v1/recovery-cases/"+caseID+"/reconcile", "application/json", nil)
	if err != nil {
		return "", false, fmt.Errorf("POST /reconcile: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("POST /reconcile: unexpected status %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		CaseStatus string `json:"case_status"`
		Applied    bool   `json:"applied"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", false, fmt.Errorf("decode /reconcile response: %w", err)
	}
	return out.CaseStatus, out.Applied, nil
}

func (c *demoClient) postSignedWebhook(secret, body string) (bool, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/webhooks/razorpay", bytes.NewReader([]byte(body)))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", signature)
	req.Header.Set("X-Razorpay-Event-Id", "evt_demo_webhook_"+uuid.New().String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("POST /v1/webhooks/razorpay: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("POST /v1/webhooks/razorpay: unexpected status %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		Matched bool `json:"matched"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, fmt.Errorf("decode webhook response: %w", err)
	}
	return out.Matched, nil
}
