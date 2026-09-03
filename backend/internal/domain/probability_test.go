package domain_test

import (
	"errors"
	"testing"

	"revguard/backend/internal/domain"
)

func TestNewProbabilityBasisPoints_Accepted(t *testing.T) {
	for _, bps := range []int{0, 1, 5000, 9999, 10000} {
		got, err := domain.NewProbabilityBasisPoints(bps)
		if err != nil {
			t.Errorf("NewProbabilityBasisPoints(%d): unexpected error: %v", bps, err)
		}
		if int(got) != bps {
			t.Errorf("NewProbabilityBasisPoints(%d): got %d", bps, int(got))
		}
	}
}

func TestNewProbabilityBasisPoints_Rejected(t *testing.T) {
	for _, bps := range []int{-1, -5000, 10001, 20000} {
		_, err := domain.NewProbabilityBasisPoints(bps)
		if !errors.Is(err, domain.ErrInvalidProbability) {
			t.Errorf("NewProbabilityBasisPoints(%d): expected ErrInvalidProbability, got %v", bps, err)
		}
	}
}
