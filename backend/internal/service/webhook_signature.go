package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// WebhookSignatureVerifier authenticates an inbound webhook request before
// its payload is trusted. Verification is deliberately a dedicated
// component, not mixed into WebhookProcessor's business logic — the same
// separation AIClient/PaymentProvider maintain between transport and
// orchestration concerns.
//
// Verify must operate on the exact raw request body (not a re-marshaled or
// re-parsed version of it — re-serializing JSON can change byte-for-byte
// content in ways that would make a valid signature appear invalid, or
// worse, mask a tampered payload) and must never accept a signature
// carried inside the parsed payload itself, only from the transport-level
// header the provider actually signed.
type WebhookSignatureVerifier interface {
	Verify(rawBody []byte, signatureHeader string) error
}

// RazorpayWebhookVerifier implements Razorpay's documented webhook
// signature scheme: HMAC-SHA256 of the exact raw request body, keyed with
// the merchant's configured webhook secret, hex-encoded, and sent in the
// `X-Razorpay-Signature` header. This is the same long-stable mechanism
// Stripe/GitHub/most webhook providers use (HMAC over the raw body,
// hex-encoded, constant-time compared) — written from Razorpay's
// publicly documented behavior, NOT re-verified against current live
// documentation or a real webhook delivery in this session (see
// docs/architecture/webhooks-reconciliation.md for the same honesty
// caveat already applied to RazorpayProvider/RazorpayReconciler).
type RazorpayWebhookVerifier struct {
	secret []byte
}

// NewRazorpayWebhookVerifier builds a verifier from the configured webhook
// secret (RAZORPAY_WEBHOOK_SECRET). It intentionally does NOT accept an
// empty secret — see NewConfiguredWebhookVerifier, which is what
// cmd/server/main.go actually calls, for the fail-closed behavior when no
// secret is configured at all.
func NewRazorpayWebhookVerifier(secret string) (*RazorpayWebhookVerifier, error) {
	if secret == "" {
		return nil, fmt.Errorf("service: RazorpayWebhookVerifier requires a non-empty secret")
	}
	return &RazorpayWebhookVerifier{secret: []byte(secret)}, nil
}

func (v *RazorpayWebhookVerifier) Verify(rawBody []byte, signatureHeader string) error {
	if signatureHeader == "" {
		return fmt.Errorf("%w: missing signature header", ErrInvalidWebhookSignature)
	}
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison — a naive == comparison would leak timing
	// information about how many leading bytes matched, which is exactly
	// the kind of side channel HMAC verification exists to avoid.
	if !hmac.Equal([]byte(expected), []byte(signatureHeader)) {
		return fmt.Errorf("%w: signature does not match", ErrInvalidWebhookSignature)
	}
	return nil
}

// alwaysRejectVerifier fails every verification attempt with a fixed
// reason. Used when no webhook secret is configured, so a misconfigured
// deployment fails LOUDLY and CLOSED (every webhook rejected, clearly
// logged) rather than silently accepting unsigned/unverifiable payloads —
// per the explicit requirement that a missing secret must never disable
// verification.
type alwaysRejectVerifier struct {
	reason string
}

func (v *alwaysRejectVerifier) Verify([]byte, string) error {
	return fmt.Errorf("%w: %s", ErrInvalidWebhookSignature, v.reason)
}

// NewConfiguredWebhookVerifier builds the verifier cmd/server/main.go
// actually wires up: a real RazorpayWebhookVerifier when a secret is
// configured, or a verifier that rejects every request when it is not.
// This is what makes "fail safely rather than silently disabling
// verification" true at the composition root, without requiring every
// caller to remember to check for an empty secret themselves.
func NewConfiguredWebhookVerifier(secret string) WebhookSignatureVerifier {
	if secret == "" {
		return &alwaysRejectVerifier{reason: "RAZORPAY_WEBHOOK_SECRET is not configured"}
	}
	v, err := NewRazorpayWebhookVerifier(secret)
	if err != nil {
		// Unreachable given the empty-string check above, but never
		// silently fall back to accepting everything.
		return &alwaysRejectVerifier{reason: err.Error()}
	}
	return v
}
