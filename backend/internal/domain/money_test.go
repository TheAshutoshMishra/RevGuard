package domain_test

import (
	"errors"
	"testing"

	"revguard/backend/internal/domain"
)

func TestNewMoney_NormalAmount(t *testing.T) {
	m, err := domain.NewMoney(49950, "INR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.MinorUnits != 49950 || m.Currency != "INR" {
		t.Fatalf("unexpected money: %+v", m)
	}
}

func TestNewMoney_Zero(t *testing.T) {
	m, err := domain.NewMoney(0, "INR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.MinorUnits != 0 {
		t.Fatalf("expected 0, got %d", m.MinorUnits)
	}
}

func TestNewMoney_LargeValue(t *testing.T) {
	// Larger than fits in int32, to confirm int64 is actually used.
	const large = 9_223_372_036_854 // well within int64 range
	m, err := domain.NewMoney(large, "INR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.MinorUnits != large {
		t.Fatalf("expected %d, got %d", large, m.MinorUnits)
	}
}

func TestNewMoney_CurrencyPreserved(t *testing.T) {
	m, err := domain.NewMoney(100, "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Currency != "USD" {
		t.Fatalf("expected USD, got %s", m.Currency)
	}
}

func TestNewMoney_NegativeRejected(t *testing.T) {
	_, err := domain.NewMoney(-1, "INR")
	if !errors.Is(err, domain.ErrNegativeAmount) {
		t.Fatalf("expected ErrNegativeAmount, got %v", err)
	}
}

func TestNewMoney_InvalidCurrencyRejected(t *testing.T) {
	cases := []string{"", "IN", "INRX", "inr", "123"}
	for _, c := range cases {
		if _, err := domain.NewMoney(100, domain.Currency(c)); !errors.Is(err, domain.ErrInvalidCurrency) {
			t.Errorf("currency %q: expected ErrInvalidCurrency, got %v", c, err)
		}
	}
}
