package service_test

import (
	"testing"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/service"
)

func TestValidateTransition_AllValidEdges(t *testing.T) {
	valid := []struct {
		from, to domain.RecoveryCaseStatus
	}{
		{domain.RecoveryCaseStatusDetected, domain.RecoveryCaseStatusAnalyzing},
		{domain.RecoveryCaseStatusAnalyzing, domain.RecoveryCaseStatusAnalyzed},
		{domain.RecoveryCaseStatusAnalyzed, domain.RecoveryCaseStatusPolicyCheck},
		{domain.RecoveryCaseStatusPolicyCheck, domain.RecoveryCaseStatusAllow},
		{domain.RecoveryCaseStatusPolicyCheck, domain.RecoveryCaseStatusBlock},
		{domain.RecoveryCaseStatusPolicyCheck, domain.RecoveryCaseStatusEscalate},
		{domain.RecoveryCaseStatusAllow, domain.RecoveryCaseStatusExecuting},
		{domain.RecoveryCaseStatusExecuting, domain.RecoveryCaseStatusVerifying},
		{domain.RecoveryCaseStatusVerifying, domain.RecoveryCaseStatusSuccess},
		{domain.RecoveryCaseStatusVerifying, domain.RecoveryCaseStatusFailed},
		{domain.RecoveryCaseStatusVerifying, domain.RecoveryCaseStatusUnknown},
		{domain.RecoveryCaseStatusSuccess, domain.RecoveryCaseStatusClosed},
		{domain.RecoveryCaseStatusFailed, domain.RecoveryCaseStatusClosed},
		{domain.RecoveryCaseStatusBlock, domain.RecoveryCaseStatusClosed},
	}
	for _, tc := range valid {
		if err := service.ValidateTransition(tc.from, tc.to); err != nil {
			t.Errorf("expected %s -> %s to be valid, got error: %v", tc.from, tc.to, err)
		}
	}
}

func TestValidateTransition_RejectsInvalidEdges(t *testing.T) {
	invalid := []struct {
		from, to domain.RecoveryCaseStatus
	}{
		{domain.RecoveryCaseStatusDetected, domain.RecoveryCaseStatusSuccess},
		{domain.RecoveryCaseStatusAnalyzing, domain.RecoveryCaseStatusExecuting},
		{domain.RecoveryCaseStatusSuccess, domain.RecoveryCaseStatusAnalyzing},
		{domain.RecoveryCaseStatusDetected, domain.RecoveryCaseStatusClosed},
		{domain.RecoveryCaseStatusClosed, domain.RecoveryCaseStatusDetected},
		{domain.RecoveryCaseStatusEscalate, domain.RecoveryCaseStatusClosed},
		{domain.RecoveryCaseStatusUnknown, domain.RecoveryCaseStatusClosed},
		{domain.RecoveryCaseStatusAnalyzed, domain.RecoveryCaseStatusAllow},
	}
	for _, tc := range invalid {
		if err := service.ValidateTransition(tc.from, tc.to); err == nil {
			t.Errorf("expected %s -> %s to be rejected, got nil error", tc.from, tc.to)
		}
	}
}
