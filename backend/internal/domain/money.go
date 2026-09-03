package domain

import (
	"errors"
	"fmt"
)

// Currency is an ISO 4217 three-letter currency code (e.g. "INR", "USD").
type Currency string

// ErrInvalidCurrency is returned when a currency code is not a 3-letter
// uppercase ISO 4217-shaped code.
var ErrInvalidCurrency = errors.New("domain: currency must be a 3-letter uppercase ISO 4217 code")

// NewCurrency validates and returns a Currency.
func NewCurrency(code string) (Currency, error) {
	if len(code) != 3 {
		return "", ErrInvalidCurrency
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return "", ErrInvalidCurrency
		}
	}
	return Currency(code), nil
}

// Money represents a financial amount as an integer count of the currency's
// minor units (e.g. paise for INR, cents for USD).
//
// Amounts are never represented as float/double: floating-point arithmetic
// is not exact for decimal currency values and can silently misstate money
// after repeated operations. ₹499.50 is stored as MinorUnits=49950 with
// Currency="INR". Callers are responsible for knowing each currency's minor
// unit exponent (2 for INR/USD, 0 for JPY, etc.) when formatting for
// display; that formatting concern is out of scope for the domain layer.
type Money struct {
	MinorUnits int64
	Currency   Currency
}

// ErrNegativeAmount is returned when a Money value would be negative.
var ErrNegativeAmount = errors.New("domain: money amount must not be negative")

// NewMoney validates and returns a Money value. Amounts must be
// non-negative; refunds/reversals are modeled as separate domain events
// rather than negative amounts.
func NewMoney(minorUnits int64, currency Currency) (Money, error) {
	if minorUnits < 0 {
		return Money{}, ErrNegativeAmount
	}
	if _, err := NewCurrency(string(currency)); err != nil {
		return Money{}, err
	}
	return Money{MinorUnits: minorUnits, Currency: currency}, nil
}

func (m Money) String() string {
	return fmt.Sprintf("%d %s", m.MinorUnits, m.Currency)
}
