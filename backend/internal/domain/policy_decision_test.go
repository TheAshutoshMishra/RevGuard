package domain_test

import (
	"testing"

	"revguard/backend/internal/domain"
)

func TestPolicyDecisionOutcome_Valid(t *testing.T) {
	for _, o := range domain.ValidPolicyDecisionOutcomes {
		if !o.Valid() {
			t.Errorf("expected %q to be valid", o)
		}
	}
}

func TestPolicyDecisionOutcome_Invalid(t *testing.T) {
	for _, o := range []domain.PolicyDecisionOutcome{"", "allow", "MAYBE", "PENDING"} {
		if o.Valid() {
			t.Errorf("expected %q to be invalid", o)
		}
	}
}

func TestPolicyDecisionOutcome_MatchesRecoveryCaseStatusStrings(t *testing.T) {
	// PolicyDecisionOutcome's values are deliberately identical strings
	// to the corresponding RecoveryCaseStatus values — see the type's
	// doc comment. This test guards that intentional coupling: if either
	// vocabulary drifts, the direct string-cast in PolicyEngine.Evaluate
	// silently breaks.
	cases := map[domain.PolicyDecisionOutcome]domain.RecoveryCaseStatus{
		domain.PolicyDecisionOutcomeAllow:    domain.RecoveryCaseStatusAllow,
		domain.PolicyDecisionOutcomeBlock:    domain.RecoveryCaseStatusBlock,
		domain.PolicyDecisionOutcomeEscalate: domain.RecoveryCaseStatusEscalate,
	}
	for outcome, status := range cases {
		if string(outcome) != string(status) {
			t.Errorf("PolicyDecisionOutcome %q does not match RecoveryCaseStatus %q", outcome, status)
		}
	}
}

func TestPolicyReasonCode_Valid(t *testing.T) {
	for _, c := range domain.ValidPolicyReasonCodes {
		if !c.Valid() {
			t.Errorf("expected %q to be valid", c)
		}
	}
}

func TestPolicyReasonCode_Invalid(t *testing.T) {
	for _, c := range []domain.PolicyReasonCode{"", "low_confidence", "SOME_OTHER_REASON"} {
		if c.Valid() {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}
