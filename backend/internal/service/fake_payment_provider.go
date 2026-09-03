package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
)

// FakeProviderScenario selects FakeProvider's deterministic behavior.
type FakeProviderScenario string

const (
	// FakeProviderScenarioSuccess returns a definitive success.
	FakeProviderScenarioSuccess FakeProviderScenario = "success"
	// FakeProviderScenarioDefinitiveFailure returns a definitive,
	// provider-declined failure (e.g. "card declined").
	FakeProviderScenarioDefinitiveFailure FakeProviderScenario = "definitive_failure"
	// FakeProviderScenarioUnsupported returns a definitive failure with
	// an UNSUPPORTED_OPERATION error code — the provider understood the
	// request but cannot perform it for this payment. Still definitive,
	// not ambiguous: the provider gave a clear answer.
	FakeProviderScenarioUnsupported FakeProviderScenario = "unsupported"
	// FakeProviderScenarioTimeout simulates a request that timed out
	// before any response was received — ambiguous.
	FakeProviderScenarioTimeout FakeProviderScenario = "timeout"
	// FakeProviderScenarioTransportError simulates a network/connection
	// failure before any response was received — also ambiguous.
	FakeProviderScenarioTransportError FakeProviderScenario = "transport_error"
)

// ErrFakeProviderTimeout and ErrFakeProviderTransportError are the two
// ambiguous-outcome errors FakeProvider can return. ExecutionEngine
// treats both identically (UNKNOWN) — they're kept distinguishable here
// only so tests and audit metadata can tell which was simulated.
var (
	ErrFakeProviderTimeout        = errors.New("fake provider: simulated timeout, no response received")
	ErrFakeProviderTransportError = errors.New("fake provider: simulated transport/connection error")
)

// FakeProvider is a deterministic, in-process PaymentProvider for tests
// and local demonstration. It never calls any real gateway and always
// identifies itself as "fake" — see Name() — so a persisted RecoveryAction
// can never be mistaken for a real execution.
//
// InvocationCount is safe for concurrent use (atomic), letting
// concurrency/idempotency tests assert exactly how many times
// RetryPayment actually ran.
type FakeProvider struct {
	scenario    FakeProviderScenario
	invocations int64
}

func NewFakeProvider(scenario FakeProviderScenario) *FakeProvider {
	return &FakeProvider{scenario: scenario}
}

func (p *FakeProvider) Name() string { return "fake" }

func (p *FakeProvider) InvocationCount() int64 {
	return atomic.LoadInt64(&p.invocations)
}

func (p *FakeProvider) RetryPayment(_ context.Context, request RetryPaymentRequest) (RetryPaymentResult, error) {
	atomic.AddInt64(&p.invocations, 1)

	switch p.scenario {
	case FakeProviderScenarioSuccess:
		return RetryPaymentResult{
			Succeeded:         true,
			ProviderReference: "fake_ref_" + request.IdempotencyKey,
		}, nil
	case FakeProviderScenarioDefinitiveFailure:
		return RetryPaymentResult{
			Succeeded:    false,
			ErrorCode:    "CARD_DECLINED",
			ErrorMessage: "fake provider: card declined",
		}, nil
	case FakeProviderScenarioUnsupported:
		return RetryPaymentResult{
			Succeeded:    false,
			ErrorCode:    "UNSUPPORTED_OPERATION",
			ErrorMessage: "fake provider: retry not supported for this payment",
		}, nil
	case FakeProviderScenarioTimeout:
		return RetryPaymentResult{}, ErrFakeProviderTimeout
	case FakeProviderScenarioTransportError:
		return RetryPaymentResult{}, ErrFakeProviderTransportError
	default:
		return RetryPaymentResult{}, fmt.Errorf("fake provider: unknown scenario %q", p.scenario)
	}
}
