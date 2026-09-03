package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"revguard/backend/internal/domain"
)

// FakeReconcilerScenario selects FakeReconciler's deterministic behavior.
type FakeReconcilerScenario string

const (
	FakeReconcilerScenarioPaymentCaptured FakeReconcilerScenario = "payment_captured"
	FakeReconcilerScenarioPaymentFailed   FakeReconcilerScenario = "payment_failed"
	FakeReconcilerScenarioPaymentPending  FakeReconcilerScenario = "payment_pending"
	FakeReconcilerScenarioPaymentNotFound FakeReconcilerScenario = "payment_not_found"
	FakeReconcilerScenarioProviderTimeout FakeReconcilerScenario = "provider_timeout"
	FakeReconcilerScenarioTransportError  FakeReconcilerScenario = "provider_transport_error"
)

var (
	ErrFakeReconcilerTimeout        = errors.New("fake reconciler: simulated timeout, no response received")
	ErrFakeReconcilerTransportError = errors.New("fake reconciler: simulated transport/connection error")
)

// FakeReconciler is a deterministic, in-process PaymentReconciler for
// tests and local development — never a real network call. It is
// deliberately a distinct type from FakeProvider (Milestone 6): a
// FakeProvider scenario describes whether an *execution request*
// succeeded, a FakeReconciler scenario describes what the provider's
// *authoritative financial state* actually is. Conflating the two would
// undermine the entire "execution success != payment success" principle
// this milestone exists to enforce — see
// docs/architecture/webhooks-reconciliation.md.
type FakeReconciler struct {
	scenario         FakeReconcilerScenario
	amountMinorUnits int64
	currency         string
	invocations      int64
}

// NewFakeReconciler builds a FakeReconciler. amountMinorUnits/currency are
// only used for the payment_captured scenario.
func NewFakeReconciler(scenario FakeReconcilerScenario, amountMinorUnits int64, currency string) *FakeReconciler {
	return &FakeReconciler{scenario: scenario, amountMinorUnits: amountMinorUnits, currency: currency}
}

func (r *FakeReconciler) InvocationCount() int64 {
	return atomic.LoadInt64(&r.invocations)
}

func (r *FakeReconciler) Reconcile(_ context.Context, request ReconciliationRequest) (ReconciliationResult, error) {
	atomic.AddInt64(&r.invocations, 1)

	switch r.scenario {
	case FakeReconcilerScenarioPaymentCaptured:
		return ReconciliationResult{
			Status:                   domain.ProviderEventStatusCaptured,
			AmountMinorUnits:         r.amountMinorUnits,
			Currency:                 r.currency,
			ProviderPaymentReference: "fake_payment_" + request.ProviderReference,
			OccurredAt:               time.Now().UTC(),
		}, nil
	case FakeReconcilerScenarioPaymentFailed:
		return ReconciliationResult{Status: domain.ProviderEventStatusFailed, OccurredAt: time.Now().UTC()}, nil
	case FakeReconcilerScenarioPaymentPending:
		return ReconciliationResult{Status: domain.ProviderEventStatusPending, OccurredAt: time.Now().UTC()}, nil
	case FakeReconcilerScenarioPaymentNotFound:
		return ReconciliationResult{}, fmt.Errorf("%w: %s", ErrReconciliationReferenceNotFound, request.ProviderReference)
	case FakeReconcilerScenarioProviderTimeout:
		return ReconciliationResult{}, ErrFakeReconcilerTimeout
	case FakeReconcilerScenarioTransportError:
		return ReconciliationResult{}, ErrFakeReconcilerTransportError
	default:
		return ReconciliationResult{}, fmt.Errorf("fake reconciler: unknown scenario %q", r.scenario)
	}
}
