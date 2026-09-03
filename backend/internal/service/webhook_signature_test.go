package service_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"revguard/backend/internal/service"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestRazorpayWebhookVerifier_ValidSignature(t *testing.T) {
	v, err := service.NewRazorpayWebhookVerifier("supersecret")
	if err != nil {
		t.Fatalf("NewRazorpayWebhookVerifier: %v", err)
	}
	body := []byte(`{"event":"payment_link.paid"}`)
	if err := v.Verify(body, sign("supersecret", body)); err != nil {
		t.Fatalf("expected valid signature to verify, got %v", err)
	}
}

func TestRazorpayWebhookVerifier_InvalidSignature(t *testing.T) {
	v, err := service.NewRazorpayWebhookVerifier("supersecret")
	if err != nil {
		t.Fatalf("NewRazorpayWebhookVerifier: %v", err)
	}
	body := []byte(`{"event":"payment_link.paid"}`)
	err = v.Verify(body, "0000000000000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, service.ErrInvalidWebhookSignature) {
		t.Fatalf("expected ErrInvalidWebhookSignature, got %v", err)
	}
}

func TestRazorpayWebhookVerifier_TamperedBody(t *testing.T) {
	v, err := service.NewRazorpayWebhookVerifier("supersecret")
	if err != nil {
		t.Fatalf("NewRazorpayWebhookVerifier: %v", err)
	}
	original := []byte(`{"event":"payment_link.paid","amount":100}`)
	sig := sign("supersecret", original)
	tampered := []byte(`{"event":"payment_link.paid","amount":999999}`)
	if err := v.Verify(tampered, sig); !errors.Is(err, service.ErrInvalidWebhookSignature) {
		t.Fatalf("expected tampered body to fail verification, got %v", err)
	}
}

func TestRazorpayWebhookVerifier_MissingSignatureHeader(t *testing.T) {
	v, err := service.NewRazorpayWebhookVerifier("supersecret")
	if err != nil {
		t.Fatalf("NewRazorpayWebhookVerifier: %v", err)
	}
	if err := v.Verify([]byte(`{}`), ""); !errors.Is(err, service.ErrInvalidWebhookSignature) {
		t.Fatalf("expected missing signature header to fail, got %v", err)
	}
}

func TestNewRazorpayWebhookVerifier_EmptySecretRejected(t *testing.T) {
	if _, err := service.NewRazorpayWebhookVerifier(""); err == nil {
		t.Fatal("expected empty secret to be rejected")
	}
}

func TestNewConfiguredWebhookVerifier_NoSecretFailsClosed(t *testing.T) {
	v := service.NewConfiguredWebhookVerifier("")
	body := []byte(`{"event":"payment_link.paid"}`)
	// Even a signature that would be valid under some secret must never
	// be accepted when no secret is configured at all — fail closed, not
	// fail open.
	err := v.Verify(body, sign("anything", body))
	if !errors.Is(err, service.ErrInvalidWebhookSignature) {
		t.Fatalf("expected fail-closed rejection, got %v", err)
	}
}

func TestNewConfiguredWebhookVerifier_WithSecretVerifiesNormally(t *testing.T) {
	v := service.NewConfiguredWebhookVerifier("supersecret")
	body := []byte(`{"event":"payment_link.paid"}`)
	if err := v.Verify(body, sign("supersecret", body)); err != nil {
		t.Fatalf("expected valid signature to verify, got %v", err)
	}
	if err := v.Verify(body, sign("wrongsecret", body)); err == nil {
		t.Fatal("expected wrong-secret signature to be rejected")
	}
}
