package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"revguard/backend/internal/domain"
)

// EventInput is the raw, external event envelope accepted by the event
// ingestion boundary (HTTP today, a Redpanda consumer later). It is
// intentionally decoupled from domain.RecoveryEvent so parsing untrusted
// input has a stable, permissive shape before any domain invariant is
// enforced.
type EventInput struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	MerchantID    string          `json:"merchant_id"`
	OccurredAt    string          `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

// Validate parses and validates raw input into a domain.RecoveryEvent
// ready for persistence. It never trusts the caller: every field is
// checked before a domain.RecoveryEvent is built.
func (in EventInput) Validate() (domain.RecoveryEvent, error) {
	var out domain.RecoveryEvent

	if in.EventID == "" {
		return out, fmt.Errorf("%w: event_id is required", ErrInvalidEvent)
	}

	eventType := domain.RecoveryEventType(in.EventType)
	if !eventType.Valid() {
		return out, fmt.Errorf("%w: event_type %q is not recognized", ErrInvalidEvent, in.EventType)
	}

	if in.AggregateType == "" {
		return out, fmt.Errorf("%w: aggregate_type is required", ErrInvalidEvent)
	}

	aggregateID, err := uuid.Parse(in.AggregateID)
	if err != nil {
		return out, fmt.Errorf("%w: aggregate_id is not a valid UUID", ErrInvalidEvent)
	}

	merchantID, err := uuid.Parse(in.MerchantID)
	if err != nil {
		return out, fmt.Errorf("%w: merchant_id is not a valid UUID", ErrInvalidEvent)
	}

	if in.OccurredAt == "" {
		return out, fmt.Errorf("%w: occurred_at is required", ErrInvalidEvent)
	}
	occurredAt, err := time.Parse(time.RFC3339, in.OccurredAt)
	if err != nil {
		return out, fmt.Errorf("%w: occurred_at must be RFC3339", ErrInvalidEvent)
	}

	if len(in.Payload) == 0 || string(in.Payload) == "null" {
		return out, fmt.Errorf("%w: payload is required", ErrInvalidEvent)
	}
	var probe any
	if err := json.Unmarshal(in.Payload, &probe); err != nil {
		return out, fmt.Errorf("%w: payload is not valid JSON", ErrInvalidEvent)
	}

	out = domain.RecoveryEvent{
		ID:            uuid.New(),
		EventID:       in.EventID,
		EventType:     eventType,
		AggregateType: in.AggregateType,
		AggregateID:   aggregateID,
		MerchantID:    merchantID,
		Payload:       []byte(in.Payload),
		OccurredAt:    occurredAt,
		CreatedAt:     time.Now().UTC(),
	}
	return out, nil
}
