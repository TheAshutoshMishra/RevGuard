package service

import (
	"context"
	"log/slog"

	"revguard/backend/internal/domain"
)

// EventPublisher publishes domain events emitted by the recovery
// orchestrator (e.g. "recovery.created") to whatever transport the rest of
// the system uses to observe them. This is the clean Redpanda integration
// boundary for Milestone 2: wiring a real producer later means writing one
// new type that satisfies this interface, without touching orchestration
// logic.
type EventPublisher interface {
	Publish(ctx context.Context, event domain.RecoveryEvent) error
}

// LoggingEventPublisher publishes by structured-logging the event. It
// performs no network or durable I/O. It exists so the orchestrator has a
// concrete, non-speculative caller today.
type LoggingEventPublisher struct {
	logger *slog.Logger
}

func NewLoggingEventPublisher(logger *slog.Logger) *LoggingEventPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggingEventPublisher{logger: logger}
}

func (p *LoggingEventPublisher) Publish(_ context.Context, event domain.RecoveryEvent) error {
	p.logger.Info("domain event published",
		"event_type", string(event.EventType),
		"aggregate_type", event.AggregateType,
		"aggregate_id", event.AggregateID,
		"merchant_id", event.MerchantID,
	)
	return nil
}
