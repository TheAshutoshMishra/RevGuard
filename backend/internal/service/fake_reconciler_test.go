package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/service"
)

func TestFakeReconciler_Scenarios(t *testing.T) {
	ctx := context.Background()
	req := service.ReconciliationRequest{Provider: "fake", ProviderReference: "ref_1"}

	t.Run("captured", func(t *testing.T) {
		r := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 49950, "INR")
		result, err := r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if result.Status != domain.ProviderEventStatusCaptured {
			t.Fatalf("expected CAPTURED, got %s", result.Status)
		}
		if result.AmountMinorUnits != 49950 || result.Currency != "INR" {
			t.Fatalf("unexpected amount/currency: %d %s", result.AmountMinorUnits, result.Currency)
		}
		if result.ProviderPaymentReference == "" {
			t.Fatal("expected a non-empty provider payment reference")
		}
	})

	t.Run("failed", func(t *testing.T) {
		r := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentFailed, 0, "INR")
		result, err := r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if result.Status != domain.ProviderEventStatusFailed {
			t.Fatalf("expected FAILED, got %s", result.Status)
		}
	})

	t.Run("pending", func(t *testing.T) {
		r := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentPending, 0, "INR")
		result, err := r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if result.Status != domain.ProviderEventStatusPending {
			t.Fatalf("expected PENDING, got %s", result.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		r := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentNotFound, 0, "INR")
		_, err := r.Reconcile(ctx, req)
		if !errors.Is(err, service.ErrReconciliationReferenceNotFound) {
			t.Fatalf("expected ErrReconciliationReferenceNotFound, got %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		r := service.NewFakeReconciler(service.FakeReconcilerScenarioProviderTimeout, 0, "INR")
		_, err := r.Reconcile(ctx, req)
		if !errors.Is(err, service.ErrFakeReconcilerTimeout) {
			t.Fatalf("expected ErrFakeReconcilerTimeout, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		r := service.NewFakeReconciler(service.FakeReconcilerScenarioTransportError, 0, "INR")
		_, err := r.Reconcile(ctx, req)
		if !errors.Is(err, service.ErrFakeReconcilerTransportError) {
			t.Fatalf("expected ErrFakeReconcilerTransportError, got %v", err)
		}
	})
}

func TestFakeReconciler_InvocationCount(t *testing.T) {
	r := service.NewFakeReconciler(service.FakeReconcilerScenarioPaymentCaptured, 100, "INR")
	req := service.ReconciliationRequest{Provider: "fake", ProviderReference: "ref_1"}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Reconcile(context.Background(), req)
		}()
	}
	wg.Wait()

	if r.InvocationCount() != 20 {
		t.Fatalf("expected 20 invocations, got %d", r.InvocationCount())
	}
}
