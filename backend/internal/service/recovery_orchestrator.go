package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// qualifyingEventTypes are the revenue-risk signals that open or advance a
// RecoveryCase. payment.succeeded is a positive signal, not a risk one,
// and the recovery.* lifecycle types are emitted BY this system rather
// than ingested to drive it — neither qualifies.
var qualifyingEventTypes = map[domain.RecoveryEventType]bool{
	domain.RecoveryEventTypePaymentFailed:      true,
	domain.RecoveryEventTypeCheckoutAbandoned:  true,
	domain.RecoveryEventTypeSubscriptionFailed: true,
	domain.RecoveryEventTypeMandateFailed:      true,
	domain.RecoveryEventTypeInvoiceOverdue:     true,
}

// IsQualifyingEventType reports whether an event type opens or advances a
// RecoveryCase.
func IsQualifyingEventType(t domain.RecoveryEventType) bool {
	return qualifyingEventTypes[t]
}

// qualifyingOutcome describes what HandleQualifyingEvent did.
type qualifyingOutcome struct {
	Case         *domain.RecoveryCase
	CaseCreated  bool
	Transitioned bool
}

// RecoveryOrchestrator correlates a qualifying revenue-risk event to a
// RecoveryCase, creating one if none is open for the underlying payment,
// and drives the deterministic part of the state machine.
//
// Milestone 2 boundary: after creating a case (DETECTED) the orchestrator
// advances it exactly one step to ANALYZING and stops. It does not call
// the AI service, apply policy, or execute anything — those belong to
// Milestone 3 and beyond.
type RecoveryOrchestrator struct {
	logger *slog.Logger
}

func NewRecoveryOrchestrator(logger *slog.Logger) *RecoveryOrchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &RecoveryOrchestrator{logger: logger}
}

// HandleQualifyingEvent correlates `event` (already known to be a
// qualifying revenue-risk event and already durably persisted by the
// caller within `tx`) to a RecoveryCase, creating one if none is open for
// the underlying payment. All writes happen against `tx` so they commit
// or roll back atomically with the event insert.
func (o *RecoveryOrchestrator) HandleQualifyingEvent(ctx context.Context, tx pgx.Tx, event domain.RecoveryEvent) (*qualifyingOutcome, error) {
	// Milestone 1 only modeled Payment as a resolvable financial
	// aggregate; subscription/invoice/mandate do not exist as first-class
	// entities yet. Rather than silently mishandling those aggregate
	// types, reject them explicitly until a future milestone models them.
	if event.AggregateType != "payment" {
		return nil, fmt.Errorf("%w: aggregate_type %q", ErrUnsupportedAggregate, event.AggregateType)
	}

	paymentRepo := repository.NewPostgresPaymentRepository(tx)
	payment, err := paymentRepo.GetByID(ctx, event.AggregateID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("%w: payment %s", ErrAggregateNotFound, event.AggregateID)
	}
	if err != nil {
		return nil, fmt.Errorf("service: load payment: %w", err)
	}
	if payment.MerchantID != event.MerchantID {
		return nil, fmt.Errorf("%w: payment %s belongs to merchant %s, event claims %s",
			ErrMerchantMismatch, payment.ID, payment.MerchantID, event.MerchantID)
	}

	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)
	now := time.Now().UTC()

	existing, err := caseRepo.GetOpenByPaymentID(ctx, payment.ID)
	if err == nil {
		// An open case already exists for this payment. This event is
		// corroborating evidence for an already-open case: the
		// DETECTED -> ANALYZING transition is a one-time entry action
		// for a freshly created case, not something repeated per event.
		o.logger.Info("event attached to existing open recovery case",
			"event_id", event.EventID, "event_type", string(event.EventType),
			"recovery_case_id", existing.ID, "current_state", string(existing.Status))
		return &qualifyingOutcome{Case: existing}, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: lookup open recovery case: %w", err)
	}

	newCase := &domain.RecoveryCase{
		ID:            uuid.New(),
		MerchantID:    payment.MerchantID,
		CustomerID:    payment.CustomerID,
		PaymentID:     payment.ID,
		Status:        domain.RecoveryCaseStatusDetected,
		RevenueAtRisk: payment.Amount,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// The INSERT below can race a concurrent transaction inserting the
	// same payment's open case and lose (repository.IsUniqueViolation).
	// PostgreSQL poisons an entire transaction after any error until it
	// is rolled back, so the attempt is wrapped in a SAVEPOINT (a nested
	// transaction via tx.Begin on an existing pgx.Tx): losing the race
	// only rolls back the savepoint, leaving the outer transaction (and
	// the already-persisted event insert) usable for the recovery query
	// below and the eventual commit.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin savepoint: %w", err)
	}
	if err := repository.NewPostgresRecoveryCaseRepository(sp).Create(ctx, newCase); err != nil {
		_ = sp.Rollback(ctx)
		if repository.IsUniqueViolation(err) {
			// Lost a race with a concurrent transaction that created the
			// open case for this payment first. Re-read it: our event
			// still needs to be linked to *a* case, just not one we
			// created or get to transition.
			existing, rerr := caseRepo.GetOpenByPaymentID(ctx, payment.ID)
			if rerr != nil {
				return nil, fmt.Errorf("service: reload recovery case after race: %w", rerr)
			}
			o.logger.Info("lost race to create recovery case; attached to winner",
				"event_id", event.EventID, "recovery_case_id", existing.ID)
			return &qualifyingOutcome{Case: existing}, nil
		}
		return nil, fmt.Errorf("service: create recovery case: %w", err)
	}
	if err := sp.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit savepoint: %w", err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: newCase.ID,
		EventType:      "recovery_case.created",
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"status":                string(newCase.Status),
			"triggering_event_id":   event.EventID,
			"triggering_event_type": string(event.EventType),
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit case creation: %w", err)
	}

	from, to := domain.RecoveryCaseStatusDetected, domain.RecoveryCaseStatusAnalyzing
	if err := ValidateTransition(from, to); err != nil {
		// The transition table is static and this edge is declared
		// above; reaching this would mean the table and this call site
		// have drifted apart, which is a programming error, not a
		// runtime condition.
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}
	if err := caseRepo.UpdateStatus(ctx, newCase.ID, from, to, now); err != nil {
		return nil, fmt.Errorf("service: transition recovery case: %w", err)
	}
	newCase.Status = to
	newCase.UpdatedAt = now

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: newCase.ID,
		EventType:      "recovery_case.transitioned",
		ActorType:      domain.AuditActorTypeSystem,
		Metadata: auditJSON(map[string]any{
			"from":   string(from),
			"to":     string(to),
			"reason": "case detected; awaiting analysis (AI integration is Milestone 3)",
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit transition: %w", err)
	}

	o.logger.Info("recovery case created and transitioned",
		"event_id", event.EventID, "event_type", string(event.EventType),
		"recovery_case_id", newCase.ID, "merchant_id", newCase.MerchantID,
		"from_state", string(from), "to_state", string(to))

	return &qualifyingOutcome{Case: newCase, CaseCreated: true, Transitioned: true}, nil
}

func auditJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
