package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// WebhookOutcome describes what Process did with one inbound webhook
// delivery.
type WebhookOutcome struct {
	Event *domain.ProviderWebhookEvent
	// Duplicate is true when this exact provider event (same Provider +
	// ProviderEventID) was already durably ingested by an earlier
	// delivery — a safe, expected no-op for Razorpay's at-least-once
	// redelivery, never an error.
	Duplicate bool
	// FinancialOutcomeApplied is true only when this call actually
	// transitioned the correlated RecoveryCase out of VERIFYING. False
	// covers every other case: unmatched event, inconclusive (PENDING)
	// observation, or a definitive observation that arrived after the
	// case was already resolved.
	FinancialOutcomeApplied bool
	// Case reflects the RecoveryCase's status after this call, only set
	// when the event was matched to a RecoveryAction.
	Case *domain.RecoveryCase
}

// WebhookProcessor turns a raw, untrusted inbound webhook request into a
// durably ingested, idempotent ProviderWebhookEvent and — only when it
// carries definitive evidence for a RecoveryAction this system actually
// executed — establishes financial truth via applyFinancialOutcome, the
// same function ReconciliationEngine uses. Sharing that function is what
// guarantees webhook and reconciliation evidence are reconciled through
// identical, once-only, monotonic logic rather than two subtly different
// code paths. See docs/architecture/webhooks-reconciliation.md.
//
// Process never trusts anything about the payload until Verify succeeds
// against the exact raw body, never advances a RecoveryCase based on a
// caller-supplied recovery_case_id (correlation happens only via the
// provider-side reference the RecoveryAction itself recorded during
// execution), and never guesses a financial outcome from a PENDING or
// otherwise inconclusive observation.
type WebhookProcessor struct {
	pool     *pgxpool.Pool
	verifier WebhookSignatureVerifier
	parser   ProviderEventParser
	logger   *slog.Logger
}

func NewWebhookProcessor(pool *pgxpool.Pool, verifier WebhookSignatureVerifier, parser ProviderEventParser, logger *slog.Logger) *WebhookProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookProcessor{pool: pool, verifier: verifier, parser: parser, logger: logger}
}

// Process authenticates, parses, and idempotently ingests one inbound
// webhook delivery. rawBody must be the exact, unmodified request body.
//
// A signature or parse failure returns immediately with no database
// write at all — an unauthenticated or malformed request is never given
// the chance to influence durable state, not even as an ingestion row.
func (p *WebhookProcessor) Process(ctx context.Context, rawBody []byte, signatureHeader, eventIDHeader string) (*WebhookOutcome, error) {
	if err := p.verifier.Verify(rawBody, signatureHeader); err != nil {
		p.logger.Warn("webhook signature rejected", "error", err.Error())
		return nil, err
	}

	parsed, err := p.parser.Parse(rawBody, eventIDHeader)
	if err != nil {
		p.logger.Warn("webhook payload malformed", "error", err.Error())
		return nil, err
	}

	return p.ingest(ctx, parsed)
}

func (p *WebhookProcessor) ingest(ctx context.Context, parsed *ParsedProviderEvent) (*WebhookOutcome, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	webhookRepo := repository.NewPostgresProviderWebhookEventRepository(tx)
	actionRepo := repository.NewPostgresRecoveryActionRepository(tx)
	caseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	auditRepo := repository.NewPostgresAuditEventRepository(tx)

	now := time.Now().UTC()

	// Correlation happens only via the provider-side reference the
	// RecoveryAction itself recorded during execution (Milestone 6) —
	// never via any identifier the webhook payload might claim belongs
	// to a particular recovery case.
	var (
		action  *domain.RecoveryAction
		matched bool
	)
	if parsed.ProviderReference != "" {
		a, err := actionRepo.GetByProviderReference(ctx, parsed.Provider, parsed.ProviderReference)
		if err == nil {
			action = a
			matched = true
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: correlate webhook to recovery action: %w", err)
		}
	}

	event := &domain.ProviderWebhookEvent{
		ID: uuid.New(), Provider: parsed.Provider, ProviderEventID: parsed.ProviderEventID,
		EventType: parsed.EventType, ProviderReference: parsed.ProviderReference,
		Status: parsed.Status, AmountMinorUnits: parsed.AmountMinorUnits, Currency: parsed.Currency,
		OccurredAt: parsed.OccurredAt, Matched: matched,
		Metadata:   auditJSON(map[string]any{"event_type": parsed.EventType, "matched": matched}),
		ReceivedAt: now, CreatedAt: now,
	}
	if matched {
		event.RecoveryActionID = &action.ID
	}

	created, err := webhookRepo.TryCreate(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("service: persist provider webhook event: %w", err)
	}
	if !created {
		// Duplicate delivery of an already-ingested event. Reload it and
		// return, having written nothing new at all — the strongest form
		// of idempotency: not even a second audit row for the duplicate
		// itself (the original ingestion already recorded everything
		// worth recording).
		existing, err := webhookRepo.GetByProviderEventID(ctx, parsed.Provider, parsed.ProviderEventID)
		if err != nil {
			return nil, fmt.Errorf("service: reload duplicate provider webhook event: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		p.logger.Info("duplicate webhook ignored",
			"provider", parsed.Provider, "provider_event_id", parsed.ProviderEventID)
		return &WebhookOutcome{Event: existing, Duplicate: true}, nil
	}

	if !matched {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		p.logger.Info("webhook received but not correlated to any recovery action",
			"provider", parsed.Provider, "provider_event_id", parsed.ProviderEventID,
			"provider_reference", parsed.ProviderReference)
		return &WebhookOutcome{Event: event}, nil
	}

	recoveryCase, err := caseRepo.GetByID(ctx, action.RecoveryCaseID)
	if err != nil {
		return nil, fmt.Errorf("service: load recovery case for matched webhook: %w", err)
	}

	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID: uuid.New(), RecoveryCaseID: action.RecoveryCaseID, EventType: "webhook.received",
		ActorType: domain.AuditActorTypeWebhook,
		Metadata: auditJSON(map[string]any{
			"provider": parsed.Provider, "provider_event_id": parsed.ProviderEventID,
			"event_type": parsed.EventType, "status": string(parsed.Status),
			"recovery_action_id": action.ID,
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit webhook received: %w", err)
	}

	if parsed.Status == domain.ProviderEventStatusPending {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		p.logger.Info("webhook observation inconclusive (PENDING); case unchanged",
			"recovery_case_id", action.RecoveryCaseID, "recovery_action_id", action.ID)
		return &WebhookOutcome{Event: event, Case: recoveryCase}, nil
	}

	targetCaseStatus, outcomeStatus, resolvedActionStatus, err := mapProviderEventToOutcome(parsed.Status)
	if err != nil {
		return nil, fmt.Errorf("service: unreachable: %w", err)
	}

	recoveredAmount, ok := computeRecoveredAmount(outcomeStatus, parsed.AmountMinorUnits, parsed.Currency, recoveryCase.RevenueAtRisk.Currency)
	if !ok {
		// A CAPTURED event with no definitive amount/currency is not
		// strong enough evidence to fabricate a SUCCESS outcome — see
		// the financial outcome rule in
		// docs/architecture/webhooks-reconciliation.md.
		if err := auditRepo.Create(ctx, &domain.AuditEvent{
			ID: uuid.New(), RecoveryCaseID: action.RecoveryCaseID, EventType: "webhook.ignored_insufficient_evidence",
			ActorType: domain.AuditActorTypeWebhook,
			Metadata: auditJSON(map[string]any{
				"reason":             "captured_event_missing_definitive_amount_or_currency",
				"recovery_action_id": action.ID,
			}),
			CreatedAt: now,
		}); err != nil {
			return nil, fmt.Errorf("service: audit insufficient evidence: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		p.logger.Warn("webhook reported captured but carried no definitive amount/currency; not guessed into SUCCESS",
			"recovery_case_id", action.RecoveryCaseID, "recovery_action_id", action.ID)
		return &WebhookOutcome{Event: event, Case: recoveryCase}, nil
	}

	result, err := applyFinancialOutcome(ctx, tx, p.logger, financialOutcomeInput{
		RecoveryCaseID: action.RecoveryCaseID, RecoveryActionID: action.ID,
		TargetCaseStatus: targetCaseStatus, OutcomeStatus: outcomeStatus,
		RecoveredAmount: recoveredAmount, ExternalReference: parsed.ProviderReference,
		Provider: parsed.Provider, Source: domain.RecoveryOutcomeSourceWebhook,
		ProviderWebhookEventID: &event.ID, ObservedAt: parsed.OccurredAt, ActorType: domain.AuditActorTypeWebhook,
		ResolveUnknownAction: true, ResolvedActionStatus: resolvedActionStatus,
		Metadata: map[string]any{"provider_event_id": parsed.ProviderEventID, "event_type": parsed.EventType},
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	if result.Applied {
		recoveryCase.Status = targetCaseStatus
		recoveryCase.UpdatedAt = time.Now().UTC()
	}

	return &WebhookOutcome{Event: event, FinancialOutcomeApplied: result.Applied, Case: recoveryCase}, nil
}
