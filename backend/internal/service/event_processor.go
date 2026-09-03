package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// ProcessResult describes what happened to a single EventInput.
type ProcessResult struct {
	Event        domain.RecoveryEvent
	RecoveryCase *domain.RecoveryCase // nil if the event did not qualify for case creation
	Duplicate    bool
	CaseCreated  bool
	Transitioned bool

	// Diagnosis/Analyzed describe the outcome of AI analysis, attempted
	// only when this call created a new case (CaseCreated). Analyzed is
	// true only if the case was actually transitioned to ANALYZED by
	// this call. AnalysisError is set (and Diagnosis/Analyzed left zero)
	// when analysis was attempted but failed — the case remains
	// ANALYZING; this is never treated as an event-processing failure,
	// since the event itself was ingested successfully.
	Diagnosis     *domain.RecoveryDiagnosis
	Analyzed      bool
	AnalysisError string
}

// CaseAnalyzer resumes a RecoveryCase from ANALYZING via AI diagnosis.
// Defined at the point of use so EventProcessor can be tested without a
// real AI client.
type CaseAnalyzer interface {
	AnalyzeCase(ctx context.Context, recoveryCaseID uuid.UUID, triggeringEventType string) (*AnalysisOutcome, error)
}

// EventProcessor is the entry point for event ingestion: validate,
// deduplicate durably against PostgreSQL, persist, correlate to a
// RecoveryCase via the RecoveryOrchestrator, and — for a freshly created
// case — trigger AI analysis via the CaseAnalyzer. It is deliberately
// callable independently of HTTP so a future Redpanda consumer can call
// it too.
type EventProcessor struct {
	pool         *pgxpool.Pool
	orchestrator *RecoveryOrchestrator
	analyzer     CaseAnalyzer
	publisher    EventPublisher
	logger       *slog.Logger
}

func NewEventProcessor(pool *pgxpool.Pool, analyzer CaseAnalyzer, publisher EventPublisher, logger *slog.Logger) *EventProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventProcessor{
		pool:         pool,
		orchestrator: NewRecoveryOrchestrator(logger),
		analyzer:     analyzer,
		publisher:    publisher,
		logger:       logger,
	}
}

// Process validates, deduplicates, persists, and — for qualifying
// revenue-risk event types — correlates the event to a RecoveryCase. It
// never creates a second case or repeats a transition for an event_id it
// has already durably processed (see recovery_events.event_id UNIQUE).
func (p *EventProcessor) Process(ctx context.Context, input EventInput) (*ProcessResult, error) {
	event, err := input.Validate()
	if err != nil {
		return nil, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	eventRepo := repository.NewPostgresRecoveryEventRepository(tx)

	created, err := eventRepo.TryCreate(ctx, &event)
	if err != nil {
		return nil, fmt.Errorf("service: persist event: %w", err)
	}

	if !created {
		existing, err := eventRepo.GetByEventID(ctx, event.EventID)
		if err != nil {
			return nil, fmt.Errorf("service: load existing event: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}

		result := &ProcessResult{Event: *existing, Duplicate: true}
		if existing.RecoveryCaseID != nil {
			caseRepo := repository.NewPostgresRecoveryCaseRepository(p.pool)
			if c, err := caseRepo.GetByID(ctx, *existing.RecoveryCaseID); err == nil {
				result.RecoveryCase = c
			}
		}
		p.logger.Info("duplicate event ignored",
			"event_id", event.EventID, "event_type", string(event.EventType))
		return result, nil
	}

	if !IsQualifyingEventType(event.EventType) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("service: commit: %w", err)
		}
		return &ProcessResult{Event: event}, nil
	}

	outcome, err := p.orchestrator.HandleQualifyingEvent(ctx, tx, event)
	if err != nil {
		return nil, err
	}

	if err := eventRepo.SetRecoveryCaseID(ctx, event.ID, outcome.Case.ID); err != nil {
		return nil, fmt.Errorf("service: link event to case: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	// Publishing happens after commit: it is not durable-transaction
	// infrastructure, and a publish failure must never roll back state
	// that has already been committed.
	if outcome.CaseCreated {
		publishErr := p.publisher.Publish(ctx, domain.RecoveryEvent{
			EventType:     domain.RecoveryEventTypeRecoveryCreated,
			AggregateType: "recovery_case",
			AggregateID:   outcome.Case.ID,
			MerchantID:    event.MerchantID,
			OccurredAt:    time.Now().UTC(),
		})
		if publishErr != nil {
			p.logger.Warn("failed to publish recovery.created event",
				"recovery_case_id", outcome.Case.ID, "error", publishErr)
		}
	}

	caseID := outcome.Case.ID
	event.RecoveryCaseID = &caseID
	result := &ProcessResult{
		Event:        event,
		RecoveryCase: outcome.Case,
		CaseCreated:  outcome.CaseCreated,
		Transitioned: outcome.Transitioned,
	}

	// AI analysis, like publishing, happens after commit and outside any
	// database transaction: it's an external HTTP call to another
	// service. Only attempt it for a freshly created case — a case that
	// already existed is not this call's to advance (see
	// RecoveryOrchestrator). Analysis failure does not fail event
	// processing: the event was durably ingested and the case was
	// durably created regardless of whether analysis succeeds.
	if outcome.CaseCreated && p.analyzer != nil {
		analysisOutcome, analyzeErr := p.analyzer.AnalyzeCase(ctx, outcome.Case.ID, string(event.EventType))
		if analyzeErr != nil {
			p.logger.Warn("AI analysis failed; recovery case remains ANALYZING",
				"recovery_case_id", outcome.Case.ID, "error", analyzeErr)
			result.AnalysisError = analyzeErr.Error()
		} else {
			result.Analyzed = analysisOutcome.Analyzed
			result.Diagnosis = analysisOutcome.Diagnosis
			if analysisOutcome.Case != nil {
				result.RecoveryCase = analysisOutcome.Case
			}
		}
	}

	return result, nil
}
