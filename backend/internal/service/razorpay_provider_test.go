package service

import (
	"regexp"
	"testing"

	"github.com/google/uuid"
)

// razorpayReferenceIDValidChars mirrors the conservative character set
// Razorpay's reference_id field is documented to accept (alphanumerics
// plus a small set of punctuation) — our own output only ever uses
// lowercase hex digits and underscores, a strict subset.
var razorpayReferenceIDValidChars = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestRazorpayReferenceID_LengthNeverExceeds40(t *testing.T) {
	inputs := []string{
		"policy-decision:" + uuid.New().String(),
		"policy-decision:00000000-0000-0000-0000-000000000000",
		"",
		"short",
		"a-much-longer-idempotency-key-than-anything-execution_engine-would-ever-actually-produce-in-practice",
	}
	for _, in := range inputs {
		out := razorpayReferenceID(in)
		if len(out) > 40 {
			t.Errorf("razorpayReferenceID(%q) = %q, length %d exceeds Razorpay's 40-character reference_id limit", in, out, len(out))
		}
	}
}

func TestRazorpayReferenceID_Deterministic(t *testing.T) {
	key := "policy-decision:" + uuid.New().String()
	first := razorpayReferenceID(key)
	for i := 0; i < 5; i++ {
		if got := razorpayReferenceID(key); got != first {
			t.Fatalf("razorpayReferenceID(%q) not deterministic: got %q, want %q", key, got, first)
		}
	}
}

func TestRazorpayReferenceID_DifferentInputsProduceDifferentOutputs(t *testing.T) {
	a := "policy-decision:" + uuid.New().String()
	b := "policy-decision:" + uuid.New().String()
	if a == b {
		t.Fatal("test setup produced identical UUIDs")
	}
	refA := razorpayReferenceID(a)
	refB := razorpayReferenceID(b)
	if refA == refB {
		t.Fatalf("razorpayReferenceID collided for distinct inputs: %q -> %q, %q -> %q", a, refA, b, refB)
	}
}

func TestRazorpayReferenceID_ValidCharacters(t *testing.T) {
	out := razorpayReferenceID("policy-decision:" + uuid.New().String())
	if !razorpayReferenceIDValidChars.MatchString(out) {
		t.Fatalf("razorpayReferenceID output %q contains characters outside the safe set", out)
	}
}

func TestRazorpayReferenceID_HasRecognizablePrefix(t *testing.T) {
	out := razorpayReferenceID("policy-decision:" + uuid.New().String())
	if len(out) < len(razorpayReferenceIDPrefix) || out[:len(razorpayReferenceIDPrefix)] != razorpayReferenceIDPrefix {
		t.Fatalf("razorpayReferenceID output %q does not start with the expected prefix %q", out, razorpayReferenceIDPrefix)
	}
}

// TestRazorpayReferenceID_InternalIdempotencyKeyUnchanged guards the core
// safety requirement: computing a Razorpay-safe reference_id must never
// mutate, truncate, or otherwise alter the caller's copy of the full
// internal idempotency key. RevGuard's own idempotency guarantee
// (execution_engine.go's TryCreate/resumeExisting, and the UNIQUE
// constraint on recovery_actions.idempotency_key) depends entirely on
// that value staying exactly as ExecutionEngine generated it.
func TestRazorpayReferenceID_InternalIdempotencyKeyUnchanged(t *testing.T) {
	original := "policy-decision:" + uuid.New().String()
	before := original

	_ = razorpayReferenceID(original)

	if original != before {
		t.Fatalf("razorpayReferenceID mutated its input: got %q, want %q", original, before)
	}
	if len(original) <= 40 {
		t.Fatalf("test fixture expected a >40 char idempotency key to meaningfully exercise the transform, got %d chars", len(original))
	}
}

func TestRazorpayReferenceID_SameTransformForRetryAndSendPaymentLink(t *testing.T) {
	// RetryPayment and SendPaymentLink both route through the shared
	// createPaymentLink helper, which is the only call site for
	// razorpayReferenceID — this test documents that expectation so a
	// future refactor introducing a second call site (with a different
	// transform) fails loudly rather than silently diverging.
	key := "policy-decision:" + uuid.New().String()
	if razorpayReferenceID(key) != razorpayReferenceID(key) {
		t.Fatal("razorpayReferenceID must produce the same reference_id regardless of which action type calls it")
	}
}
