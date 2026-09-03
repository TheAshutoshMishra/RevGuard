package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"revguard/backend/internal/service"
)

func validEventInput() service.EventInput {
	return service.EventInput{
		EventID:       "evt-1",
		EventType:     "payment.failed",
		AggregateType: "payment",
		AggregateID:   uuid.New().String(),
		MerchantID:    uuid.New().String(),
		OccurredAt:    "2026-01-01T00:00:00Z",
		Payload:       []byte(`{"reason":"insufficient_funds"}`),
	}
}

func TestEventInputValidate_Valid(t *testing.T) {
	in := validEventInput()
	event, err := in.Validate()
	if err != nil {
		t.Fatalf("expected valid input to pass, got error: %v", err)
	}
	if event.EventID != in.EventID {
		t.Errorf("EventID mismatch: got %q want %q", event.EventID, in.EventID)
	}
	if event.ID == uuid.Nil {
		t.Error("expected a generated ID")
	}
}

func TestEventInputValidate_Rejections(t *testing.T) {
	cases := map[string]func(*service.EventInput){
		"missing event_id": func(in *service.EventInput) {
			in.EventID = ""
		},
		"invalid event_type": func(in *service.EventInput) {
			in.EventType = "not.a.real.type"
		},
		"missing aggregate_type": func(in *service.EventInput) {
			in.AggregateType = ""
		},
		"invalid aggregate_id": func(in *service.EventInput) {
			in.AggregateID = "not-a-uuid"
		},
		"invalid merchant_id": func(in *service.EventInput) {
			in.MerchantID = "not-a-uuid"
		},
		"missing occurred_at": func(in *service.EventInput) {
			in.OccurredAt = ""
		},
		"malformed occurred_at": func(in *service.EventInput) {
			in.OccurredAt = "not-a-timestamp"
		},
		"missing payload": func(in *service.EventInput) {
			in.Payload = nil
		},
		"null payload": func(in *service.EventInput) {
			in.Payload = []byte("null")
		},
		"malformed payload": func(in *service.EventInput) {
			in.Payload = []byte("{not json")
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := validEventInput()
			mutate(&in)
			_, err := in.Validate()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !errors.Is(err, service.ErrInvalidEvent) {
				t.Errorf("expected ErrInvalidEvent, got %v", err)
			}
		})
	}
}
