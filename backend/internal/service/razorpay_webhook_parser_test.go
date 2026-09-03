package service_test

import (
	"errors"
	"testing"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/service"
)

func TestRazorpayWebhookParser_PaymentLinkPaid(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	body := []byte(`{
		"event": "payment_link.paid",
		"payload": {
			"payment_link": {"entity": {"id": "plink_abc123", "status": "paid"}},
			"payment": {"entity": {"id": "pay_xyz789", "amount": 49950, "currency": "INR", "status": "captured"}}
		},
		"created_at": 1700000000
	}`)

	parsed, err := p.Parse(body, "evt_1")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Provider != "razorpay" {
		t.Fatalf("expected provider razorpay, got %q", parsed.Provider)
	}
	if parsed.ProviderEventID != "evt_1" {
		t.Fatalf("expected event id from header, got %q", parsed.ProviderEventID)
	}
	if parsed.ProviderReference != "plink_abc123" {
		t.Fatalf("expected provider reference plink_abc123, got %q", parsed.ProviderReference)
	}
	if parsed.Status != domain.ProviderEventStatusCaptured {
		t.Fatalf("expected CAPTURED, got %s", parsed.Status)
	}
	if parsed.AmountMinorUnits != 49950 {
		t.Fatalf("expected amount 49950, got %d", parsed.AmountMinorUnits)
	}
	if parsed.Currency != "INR" {
		t.Fatalf("expected INR, got %s", parsed.Currency)
	}
}

func TestRazorpayWebhookParser_PaymentLinkCancelledAndExpired(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	for _, event := range []string{"payment_link.cancelled", "payment_link.expired"} {
		body := []byte(`{"event":"` + event + `","payload":{"payment_link":{"entity":{"id":"plink_1","status":"x"}}}}`)
		parsed, err := p.Parse(body, "evt_2")
		if err != nil {
			t.Fatalf("Parse(%s): %v", event, err)
		}
		if parsed.Status != domain.ProviderEventStatusFailed {
			t.Fatalf("expected FAILED for %s, got %s", event, parsed.Status)
		}
	}
}

func TestRazorpayWebhookParser_UnknownEventIsPending(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	body := []byte(`{"event":"payment_link.partially_paid","payload":{"payment_link":{"entity":{"id":"plink_1","status":"x"}}}}`)
	parsed, err := p.Parse(body, "evt_3")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Status != domain.ProviderEventStatusPending {
		t.Fatalf("expected PENDING for unrecognized event, got %s", parsed.Status)
	}
}

func TestRazorpayWebhookParser_MalformedJSON(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	_, err := p.Parse([]byte(`{not json`), "evt_4")
	if !errors.Is(err, service.ErrMalformedWebhookPayload) {
		t.Fatalf("expected ErrMalformedWebhookPayload, got %v", err)
	}
}

func TestRazorpayWebhookParser_MissingEventField(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	_, err := p.Parse([]byte(`{"payload":{}}`), "evt_5")
	if !errors.Is(err, service.ErrMalformedWebhookPayload) {
		t.Fatalf("expected ErrMalformedWebhookPayload, got %v", err)
	}
}

func TestRazorpayWebhookParser_MissingEventIDHeaderFallsBackToBodyHash(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	body := []byte(`{"event":"payment_link.paid","payload":{"payment_link":{"entity":{"id":"plink_1"}}}}`)

	first, err := p.Parse(body, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	second, err := p.Parse(body, "")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if first.ProviderEventID == "" {
		t.Fatal("expected a non-empty fallback event id")
	}
	if first.ProviderEventID != second.ProviderEventID {
		t.Fatalf("expected deterministic fallback event id for identical bodies, got %q vs %q",
			first.ProviderEventID, second.ProviderEventID)
	}
}

func TestRazorpayWebhookParser_UnrecognizedResourceIsUnmatched(t *testing.T) {
	p := service.NewRazorpayWebhookParser()
	body := []byte(`{"event":"payment_link.paid","payload":{}}`)
	parsed, err := p.Parse(body, "evt_6")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.ProviderReference != "" {
		t.Fatalf("expected empty provider reference, got %q", parsed.ProviderReference)
	}
}
