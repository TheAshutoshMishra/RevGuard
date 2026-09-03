package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"revguard/backend/internal/service"
)

func validRecommendationJSON(caseID uuid.UUID) []byte {
	b, _ := json.Marshal(map[string]any{
		"case_id": caseID,
		"diagnosis": map[string]any{
			"reason":               "test reason",
			"failure_category":     "transient_failure",
			"customer_context":     "test context",
			"recommended_strategy": "retry_payment",
		},
		"recommendation": map[string]any{
			"action":     "retry_payment",
			"reason":     "test reason",
			"confidence": 0.8,
		},
		"risk_flags":     []string{},
		"explanation":    "test explanation",
		"provider":       "mock",
		"model":          "mock-rule-based-v1",
		"prompt_version": "v1",
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
	})
	return b
}

func aRequest() service.AIRequest {
	return service.AIRequest{
		CaseID: uuid.New(),
		Context: service.AIRecoveryContext{
			RecoveryCaseID:      uuid.New(),
			MerchantID:          uuid.New(),
			CustomerID:          uuid.New(),
			PaymentID:           uuid.New(),
			AmountMinorUnits:    49950,
			Currency:            "INR",
			PaymentStatus:       "FAILED",
			TriggeringEventType: "payment.failed",
		},
	}
}

func TestAIClient_ValidResponse(t *testing.T) {
	req := aRequest()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(validRecommendationJSON(req.CaseID))
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	rec, err := client.Diagnose(context.Background(), req)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if rec.Recommendation.Action != "retry_payment" {
		t.Fatalf("unexpected action: %s", rec.Recommendation.Action)
	}
	if rec.Recommendation.Confidence != 0.8 {
		t.Fatalf("unexpected confidence: %v", rec.Recommendation.Confidence)
	}
}

func TestAIClient_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{not valid json"))
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisInvalidResponse) {
		t.Fatalf("expected ErrDiagnosisInvalidResponse, got %v", err)
	}
}

func TestAIClient_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisFailed) {
		t.Fatalf("expected ErrDiagnosisFailed, got %v", err)
	}
}

func TestAIClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(validRecommendationJSON(uuid.New()))
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 20*time.Millisecond, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisFailed) {
		t.Fatalf("expected ErrDiagnosisFailed (timeout), got %v", err)
	}
}

func TestAIClient_ConnectionRefused(t *testing.T) {
	// Nothing listening on this port.
	client := service.NewHTTPAIClient("http://127.0.0.1:1", 2*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisFailed) {
		t.Fatalf("expected ErrDiagnosisFailed (connection refused), got %v", err)
	}
}

func TestAIClient_ConfidenceOutOfRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.Unmarshal(validRecommendationJSON(uuid.New()), &body)
		body["recommendation"].(map[string]any)["confidence"] = 5.0
		out, _ := json.Marshal(body)
		w.Write(out)
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisInvalidResponse) {
		t.Fatalf("expected ErrDiagnosisInvalidResponse, got %v", err)
	}
}

func TestAIClient_UnknownAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.Unmarshal(validRecommendationJSON(uuid.New()), &body)
		body["recommendation"].(map[string]any)["action"] = "launch_the_missiles"
		out, _ := json.Marshal(body)
		w.Write(out)
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisInvalidResponse) {
		t.Fatalf("expected ErrDiagnosisInvalidResponse, got %v", err)
	}
}

func TestAIClient_UnknownFailureCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.Unmarshal(validRecommendationJSON(uuid.New()), &body)
		body["diagnosis"].(map[string]any)["failure_category"] = "alien_abduction"
		out, _ := json.Marshal(body)
		w.Write(out)
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisInvalidResponse) {
		t.Fatalf("expected ErrDiagnosisInvalidResponse, got %v", err)
	}
}

func TestAIClient_MissingCaseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.Unmarshal(validRecommendationJSON(uuid.New()), &body)
		delete(body, "case_id")
		out, _ := json.Marshal(body)
		w.Write(out)
	}))
	defer server.Close()

	client := service.NewHTTPAIClient(server.URL, 5*time.Second, nil)
	_, err := client.Diagnose(context.Background(), aRequest())
	if !errors.Is(err, service.ErrDiagnosisInvalidResponse) {
		t.Fatalf("expected ErrDiagnosisInvalidResponse, got %v", err)
	}
}
