package service

import (
	"fmt"

	"revguard/backend/internal/domain"
)

// allowedTransitions declares the full RecoveryCase lifecycle from the
// product spec, even though Milestone 2 only ever exercises
// DETECTED -> ANALYZING. Declaring the whole table now means later
// milestones (AI diagnosis, policy, execution, verification) call
// ValidateTransition, they don't need to edit it.
//
// ESCALATE and UNKNOWN have no outgoing transition here: how a human
// escalation resolves, and how an unknown verification outcome gets
// reconciled, are workflows that don't exist yet. They will gain outgoing
// edges when the milestone that implements them is built.
var allowedTransitions = map[domain.RecoveryCaseStatus][]domain.RecoveryCaseStatus{
	domain.RecoveryCaseStatusDetected:  {domain.RecoveryCaseStatusAnalyzing},
	domain.RecoveryCaseStatusAnalyzing: {domain.RecoveryCaseStatusAnalyzed},
	domain.RecoveryCaseStatusAnalyzed:  {domain.RecoveryCaseStatusPolicyCheck},
	domain.RecoveryCaseStatusPolicyCheck: {
		domain.RecoveryCaseStatusAllow,
		domain.RecoveryCaseStatusBlock,
		domain.RecoveryCaseStatusEscalate,
	},
	domain.RecoveryCaseStatusAllow:     {domain.RecoveryCaseStatusExecuting},
	domain.RecoveryCaseStatusExecuting: {domain.RecoveryCaseStatusVerifying},
	domain.RecoveryCaseStatusVerifying: {
		domain.RecoveryCaseStatusSuccess,
		domain.RecoveryCaseStatusFailed,
		domain.RecoveryCaseStatusUnknown,
	},
	domain.RecoveryCaseStatusSuccess: {domain.RecoveryCaseStatusClosed},
	domain.RecoveryCaseStatusFailed:  {domain.RecoveryCaseStatusClosed},
	domain.RecoveryCaseStatusBlock:   {domain.RecoveryCaseStatusClosed},
}

// ValidateTransition reports whether moving a RecoveryCase from `from` to
// `to` is permitted. It is pure and side-effect-free: it performs no I/O
// and holds no state, so it is trivially unit-testable and safe for any
// caller (HTTP handler, Redpanda consumer, future policy engine) to invoke
// directly.
func ValidateTransition(from, to domain.RecoveryCaseStatus) error {
	nextStates, ok := allowedTransitions[from]
	if !ok {
		return fmt.Errorf("%w: no transitions defined from %q", ErrInvalidTransition, from)
	}
	for _, s := range nextStates {
		if s == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %q -> %q is not a valid transition", ErrInvalidTransition, from, to)
}
