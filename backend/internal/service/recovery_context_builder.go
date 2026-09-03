package service

import (
	"context"
	"errors"
	"fmt"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// RecoveryContextBuilder assembles the minimal AIRecoveryContext sent to
// the AI service. All database access lives here (via repositories); it
// contains no HTTP logic and no orchestration decisions — see AIClient
// for the HTTP boundary and RecoveryOrchestrator for what happens with
// the result.
//
// The context is deliberately minimal: it never includes card numbers,
// CVV, authentication credentials, API keys, or any other raw payment
// secret. Milestone 1's domain.Payment doesn't model any of those fields
// in the first place, so there is nothing to accidentally leak here —
// only identifiers, amounts, statuses, and attempt/action history.
type RecoveryContextBuilder struct {
	payments repository.PaymentRepository
	attempts repository.PaymentAttemptRepository
	actions  repository.RecoveryActionRepository
}

func NewRecoveryContextBuilder(
	payments repository.PaymentRepository,
	attempts repository.PaymentAttemptRepository,
	actions repository.RecoveryActionRepository,
) *RecoveryContextBuilder {
	return &RecoveryContextBuilder{payments: payments, attempts: attempts, actions: actions}
}

// Build assembles an AIRequest for the given RecoveryCase. The case's
// triggeringEventType is passed in by the caller (RecoveryOrchestrator
// already knows it from the event that created the case) rather than
// re-derived here.
func (b *RecoveryContextBuilder) Build(ctx context.Context, recoveryCase *domain.RecoveryCase, triggeringEventType string) (AIRequest, error) {
	payment, err := b.payments.GetByID(ctx, recoveryCase.PaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return AIRequest{}, fmt.Errorf("%w: payment %s", ErrAggregateNotFound, recoveryCase.PaymentID)
		}
		return AIRequest{}, fmt.Errorf("service: load payment for context: %w", err)
	}

	attempts, err := b.attempts.ListByPaymentID(ctx, payment.ID)
	if err != nil {
		return AIRequest{}, fmt.Errorf("service: load payment attempts for context: %w", err)
	}

	actions, err := b.actions.ListByRecoveryCaseID(ctx, recoveryCase.ID)
	if err != nil {
		return AIRequest{}, fmt.Errorf("service: load recovery actions for context: %w", err)
	}

	attemptContexts := make([]AIPaymentAttemptContext, 0, len(attempts))
	for _, a := range attempts {
		attemptContexts = append(attemptContexts, AIPaymentAttemptContext{
			AttemptNumber: a.AttemptNumber,
			Status:        string(a.Status),
			FailureCode:   a.FailureCode,
			FailureReason: a.FailureReason,
		})
	}

	actionContexts := make([]AIRecoveryActionContext, 0, len(actions))
	for _, a := range actions {
		actionContexts = append(actionContexts, AIRecoveryActionContext{
			ActionType:    string(a.ActionType),
			Status:        string(a.Status),
			AttemptNumber: a.AttemptNumber,
		})
	}

	return AIRequest{
		CaseID: recoveryCase.ID,
		Context: AIRecoveryContext{
			RecoveryCaseID:          recoveryCase.ID,
			MerchantID:              recoveryCase.MerchantID,
			CustomerID:              recoveryCase.CustomerID,
			PaymentID:               payment.ID,
			AmountMinorUnits:        payment.Amount.MinorUnits,
			Currency:                string(payment.Amount.Currency),
			PaymentStatus:           string(payment.Status),
			TriggeringEventType:     triggeringEventType,
			PaymentAttempts:         attemptContexts,
			PreviousRecoveryActions: actionContexts,
		},
	}, nil
}
