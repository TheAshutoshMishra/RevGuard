package service_test

import (
	"errors"
	"testing"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/service"
)

func TestGetActionEconomics_AllRecommendedActions(t *testing.T) {
	for _, action := range domain.ValidRecommendedActions {
		t.Run(string(action), func(t *testing.T) {
			econ, err := service.GetActionEconomics(action)
			if err != nil {
				t.Fatalf("GetActionEconomics(%s): %v", action, err)
			}
			if econ.ActionCostMinorUnits < 0 {
				t.Errorf("ActionCostMinorUnits must not be negative, got %d", econ.ActionCostMinorUnits)
			}
			if econ.RiskCostBps < 0 {
				t.Errorf("RiskCostBps must not be negative, got %d", econ.RiskCostBps)
			}
		})
	}
}

func TestGetActionEconomics_UnknownActionRejected(t *testing.T) {
	_, err := service.GetActionEconomics(domain.RecommendedAction("launch_the_missiles"))
	if !errors.Is(err, service.ErrUnknownRecommendedAction) {
		t.Fatalf("expected ErrUnknownRecommendedAction, got %v", err)
	}
}

func TestGetActionEconomics_StopRecoveryIsFree(t *testing.T) {
	econ, err := service.GetActionEconomics(domain.RecommendedActionStopRecovery)
	if err != nil {
		t.Fatalf("GetActionEconomics: %v", err)
	}
	if econ.ActionCostMinorUnits != 0 || econ.RiskCostBps != 0 {
		t.Errorf("expected stop_recovery to have zero cost/risk, got %+v", econ)
	}
}
