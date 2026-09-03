package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"revguard/backend/internal/domain"
)

// --- Wire types -------------------------------------------------------
//
// These mirror ai-service/app/models/diagnosis.py exactly. Keep the two
// sides in sync: a field renamed on one side without the other breaks the
// Python -> Go boundary.

type AIPaymentAttemptContext struct {
	AttemptNumber int    `json:"attempt_number"`
	Status        string `json:"status"`
	FailureCode   string `json:"failure_code,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type AIRecoveryActionContext struct {
	ActionType    string `json:"action_type"`
	Status        string `json:"status"`
	AttemptNumber int    `json:"attempt_number"`
}

// AIRecoveryContext is the recovery context sent to the AI service. It
// must never contain card numbers, CVV, authentication credentials, API
// keys, or any other raw payment secret — see RecoveryContextBuilder,
// which is the only place this struct is constructed.
type AIRecoveryContext struct {
	RecoveryCaseID          uuid.UUID                 `json:"recovery_case_id"`
	MerchantID              uuid.UUID                 `json:"merchant_id"`
	CustomerID              uuid.UUID                 `json:"customer_id"`
	PaymentID               uuid.UUID                 `json:"payment_id"`
	AmountMinorUnits        int64                     `json:"amount_minor_units"`
	Currency                string                    `json:"currency"`
	PaymentStatus           string                    `json:"payment_status"`
	TriggeringEventType     string                    `json:"triggering_event_type"`
	PaymentAttempts         []AIPaymentAttemptContext `json:"payment_attempts"`
	PreviousRecoveryActions []AIRecoveryActionContext `json:"previous_recovery_actions"`
}

type AIRequest struct {
	CaseID  uuid.UUID         `json:"case_id"`
	Context AIRecoveryContext `json:"context"`
}

type AIDiagnosis struct {
	Reason              string `json:"reason"`
	FailureCategory     string `json:"failure_category"`
	CustomerContext     string `json:"customer_context"`
	RecommendedStrategy string `json:"recommended_strategy"`
}

type AIRecommendationDetail struct {
	Action     string  `json:"action"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// AIRecommendation is the fully-decoded, Go-validated response from the
// AI service. Constructing one outside of HTTPAIClient.Diagnose's
// validation path is not supported by design — callers only ever get one
// back after it has passed the checks in validate().
type AIRecommendation struct {
	CaseID         uuid.UUID              `json:"case_id"`
	Diagnosis      AIDiagnosis            `json:"diagnosis"`
	Recommendation AIRecommendationDetail `json:"recommendation"`
	RiskFlags      []string               `json:"risk_flags"`
	Explanation    string                 `json:"explanation"`
	Provider       string                 `json:"provider"`
	Model          string                 `json:"model"`
	PromptVersion  string                 `json:"prompt_version"`
	GeneratedAt    time.Time              `json:"generated_at"`
}

// --- Client -------------------------------------------------------

// AIClient talks to the Python AI service. It is the only place in the
// codebase that knows about HTTP transport details for that call — the
// orchestrator only ever sees Diagnose's typed request/response.
type AIClient interface {
	Diagnose(ctx context.Context, request AIRequest) (*AIRecommendation, error)
}

// HTTPAIClient is the production AIClient: it POSTs to
// {baseURL}/v1/diagnose.
type HTTPAIClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	// maxTransportRetries bounds retries to transport-level failures
	// (connection refused, DNS failure — never a non-2xx response or a
	// response that fails to parse/validate) and is deliberately small.
	// See docs/architecture/ai-diagnosis.md for the rationale.
	maxTransportRetries int
}

func NewHTTPAIClient(baseURL string, timeout time.Duration, logger *slog.Logger) *HTTPAIClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPAIClient{
		baseURL:             baseURL,
		httpClient:          &http.Client{Timeout: timeout},
		logger:              logger,
		maxTransportRetries: 1,
	}
}

func (c *HTTPAIClient) Diagnose(ctx context.Context, request AIRequest) (*AIRecommendation, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("service: marshal AI request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxTransportRetries; attempt++ {
		recommendation, transportFailed, err := c.doRequest(ctx, body)
		if err == nil {
			return recommendation, nil
		}
		lastErr = err
		if !transportFailed || ctx.Err() != nil {
			// Either a real (non-retryable) failure, or the caller's
			// context is already done — retrying would not help.
			return nil, err
		}
		c.logger.Warn("AI service transport error, retrying once",
			"attempt", attempt, "error", err)
	}
	return nil, lastErr
}

// doRequest performs a single attempt. transportFailed is true only for
// failures before any HTTP response was received (dial/connection-level)
// — the one class of error Diagnose will retry.
func (c *HTTPAIClient) doRequest(ctx context.Context, body []byte) (recommendation *AIRecommendation, transportFailed bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/diagnose", bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("%w: build request: %v", ErrDiagnosisFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, false, fmt.Errorf("%w: %v", ErrDiagnosisFailed, err)
		}
		// http.Client.Do only returns *url.Error for anything that
		// happened before a response was received (DNS, dial, TLS,
		// etc.) — that is the transport-failure class we retry.
		return nil, true, fmt.Errorf("%w: %v", ErrDiagnosisFailed, err)
	}
	defer resp.Body.Close()

	const maxBody = 1 << 20 // 1 MiB is generous for this response shape
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, false, fmt.Errorf("%w: read response body: %v", ErrDiagnosisFailed, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("%w: AI service returned HTTP %d", ErrDiagnosisFailed, resp.StatusCode)
	}

	var out AIRecommendation
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("%w: malformed JSON: %v", ErrDiagnosisInvalidResponse, err)
	}

	if err := validateRecommendation(&out); err != nil {
		return nil, false, err
	}

	return &out, false, nil
}

// validateRecommendation is Go's independent validation of the AI
// response, on top of whatever Python already validated. The AI service
// is a trusted internal component, but this is a durable-state boundary:
// Go never assumes a remote service's output is safe to persist without
// checking it itself.
func validateRecommendation(r *AIRecommendation) error {
	if r.CaseID == uuid.Nil {
		return fmt.Errorf("%w: missing case_id", ErrDiagnosisInvalidResponse)
	}
	if r.Recommendation.Confidence < 0.0 || r.Recommendation.Confidence > 1.0 {
		return fmt.Errorf("%w: confidence %v out of range [0,1]", ErrDiagnosisInvalidResponse, r.Recommendation.Confidence)
	}
	if !domain.RecommendedAction(r.Recommendation.Action).Valid() {
		return fmt.Errorf("%w: unknown recommended action %q", ErrDiagnosisInvalidResponse, r.Recommendation.Action)
	}
	if !domain.FailureCategory(r.Diagnosis.FailureCategory).Valid() {
		return fmt.Errorf("%w: unknown failure category %q", ErrDiagnosisInvalidResponse, r.Diagnosis.FailureCategory)
	}
	if r.Diagnosis.Reason == "" || r.Recommendation.Reason == "" || r.Explanation == "" {
		return fmt.Errorf("%w: missing required text field", ErrDiagnosisInvalidResponse)
	}
	if r.Provider == "" || r.Model == "" || r.PromptVersion == "" {
		return fmt.Errorf("%w: missing provider/model/prompt_version", ErrDiagnosisInvalidResponse)
	}
	if r.GeneratedAt.IsZero() {
		return fmt.Errorf("%w: missing generated_at", ErrDiagnosisInvalidResponse)
	}
	return nil
}
