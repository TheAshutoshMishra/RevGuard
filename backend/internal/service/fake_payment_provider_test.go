package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"revguard/backend/internal/service"
)

func TestFakeProvider_Success(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	result, err := p.RetryPayment(context.Background(), service.RetryPaymentRequest{IdempotencyKey: "key-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected Succeeded=true")
	}
	if result.ProviderReference == "" {
		t.Fatal("expected a non-empty provider reference")
	}
	if p.Name() != "fake" {
		t.Fatalf("expected Name()=fake, got %q", p.Name())
	}
	if p.InvocationCount() != 1 {
		t.Fatalf("expected InvocationCount()=1, got %d", p.InvocationCount())
	}
}

func TestFakeProvider_DefinitiveFailure(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioDefinitiveFailure)
	result, err := p.RetryPayment(context.Background(), service.RetryPaymentRequest{IdempotencyKey: "key-1"})
	if err != nil {
		t.Fatalf("expected no error (definitive failure is not ambiguous), got %v", err)
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded=false")
	}
	if result.ErrorCode == "" {
		t.Fatal("expected a non-empty error code")
	}
	if result.ProviderReference != "" {
		t.Fatal("expected no provider reference on failure")
	}
}

func TestFakeProvider_Unsupported(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioUnsupported)
	result, err := p.RetryPayment(context.Background(), service.RetryPaymentRequest{IdempotencyKey: "key-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded=false")
	}
	if result.ErrorCode != "UNSUPPORTED_OPERATION" {
		t.Fatalf("expected UNSUPPORTED_OPERATION, got %q", result.ErrorCode)
	}
}

func TestFakeProvider_Timeout(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioTimeout)
	_, err := p.RetryPayment(context.Background(), service.RetryPaymentRequest{IdempotencyKey: "key-1"})
	if !errors.Is(err, service.ErrFakeProviderTimeout) {
		t.Fatalf("expected ErrFakeProviderTimeout, got %v", err)
	}
}

func TestFakeProvider_TransportError(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioTransportError)
	_, err := p.RetryPayment(context.Background(), service.RetryPaymentRequest{IdempotencyKey: "key-1"})
	if !errors.Is(err, service.ErrFakeProviderTransportError) {
		t.Fatalf("expected ErrFakeProviderTransportError, got %v", err)
	}
}

func TestFakeProvider_SendPaymentLink_Success(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	result, err := p.SendPaymentLink(context.Background(), service.SendPaymentLinkRequest{IdempotencyKey: "key-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Succeeded {
		t.Fatal("expected Succeeded=true")
	}
	if result.ProviderReference == "" {
		t.Fatal("expected a non-empty provider reference")
	}
}

func TestFakeProvider_SendPaymentLink_DefinitiveFailure(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioDefinitiveFailure)
	result, err := p.SendPaymentLink(context.Background(), service.SendPaymentLinkRequest{IdempotencyKey: "key-1"})
	if err != nil {
		t.Fatalf("expected no error (definitive failure is not ambiguous), got %v", err)
	}
	if result.Succeeded {
		t.Fatal("expected Succeeded=false")
	}
	if result.ErrorCode == "" {
		t.Fatal("expected a non-empty error code")
	}
}

func TestFakeProvider_SendPaymentLink_Unsupported(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioUnsupported)
	result, err := p.SendPaymentLink(context.Background(), service.SendPaymentLinkRequest{IdempotencyKey: "key-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ErrorCode != "UNSUPPORTED_OPERATION" {
		t.Fatalf("expected UNSUPPORTED_OPERATION, got %q", result.ErrorCode)
	}
}

func TestFakeProvider_SendPaymentLink_Timeout(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioTimeout)
	_, err := p.SendPaymentLink(context.Background(), service.SendPaymentLinkRequest{IdempotencyKey: "key-1"})
	if !errors.Is(err, service.ErrFakeProviderTimeout) {
		t.Fatalf("expected ErrFakeProviderTimeout, got %v", err)
	}
}

func TestFakeProvider_SendPaymentLink_TransportError(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioTransportError)
	_, err := p.SendPaymentLink(context.Background(), service.SendPaymentLinkRequest{IdempotencyKey: "key-1"})
	if !errors.Is(err, service.ErrFakeProviderTransportError) {
		t.Fatalf("expected ErrFakeProviderTransportError, got %v", err)
	}
}

func TestFakeProvider_InvocationCountConcurrent(t *testing.T) {
	p := service.NewFakeProvider(service.FakeProviderScenarioSuccess)
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = p.RetryPayment(context.Background(), service.RetryPaymentRequest{IdempotencyKey: "concurrent"})
		}(i)
	}
	wg.Wait()
	if p.InvocationCount() != n {
		t.Fatalf("expected InvocationCount()=%d, got %d", n, p.InvocationCount())
	}
}
