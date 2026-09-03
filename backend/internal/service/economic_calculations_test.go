package service

// This file is an internal (white-box) test file — package `service`,
// not `service_test` like most other tests in this directory — so it can
// exercise the unexported calculate* formula functions directly, without
// needing a database or any other dependency.

import (
	"testing"

	"revguard/backend/internal/domain"
)

func TestExpectedGrossRecovery(t *testing.T) {
	cases := []struct {
		name           string
		revenueAtRisk  int64
		probabilityBps int
		want           int64
	}{
		{"50 percent of round number", 100000, 5000, 50000},
		{"100 percent", 49950, 10000, 49950},
		{"0 percent", 49950, 0, 0},
		{"zero revenue", 0, 8000, 0},
		{"rounds down (floor)", 100, 3333, 33}, // 100*3333/10000 = 33.33 -> 33
		{"large value", 9_000_000_000, 2500, 2_250_000_000},
		{"1 bps of large value", 10_000_000, 1, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bps, err := domain.NewProbabilityBasisPoints(tc.probabilityBps)
			if err != nil {
				t.Fatalf("NewProbabilityBasisPoints: %v", err)
			}
			got := calculateExpectedGrossRecovery(tc.revenueAtRisk, bps)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRiskCost(t *testing.T) {
	cases := []struct {
		name          string
		revenueAtRisk int64
		riskCostBps   int32
		want          int64
	}{
		{"50 bps of round number", 100000, 50, 500},
		{"zero risk", 100000, 0, 0},
		{"zero revenue", 0, 100, 0},
		{"rounds down", 999, 33, 3}, // 999*33/10000 = 3.2967 -> 3
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateRiskCost(tc.revenueAtRisk, tc.riskCostBps)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExpectedIncrementalValue(t *testing.T) {
	cases := []struct {
		name                          string
		grossRecovery, cost, riskCost int64
		want                          int64
	}{
		{"positive value: gross recovery > costs", 10000, 500, 300, 9200},
		{"zero value: gross recovery == costs", 1000, 700, 300, 0},
		{"negative value: gross recovery < costs", 100, 500, 300, -700},
		{"all zero", 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateExpectedIncrementalValue(tc.grossRecovery, tc.cost, tc.riskCost)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}
