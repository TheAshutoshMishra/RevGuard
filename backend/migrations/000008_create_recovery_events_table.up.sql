-- event_id is the stable, caller-supplied identifier used to make event
-- ingestion idempotent (e.g. across webhook retries or Redpanda redelivery).
CREATE TABLE recovery_events (
    id             UUID PRIMARY KEY,
    event_id       TEXT NOT NULL,
    event_type     TEXT NOT NULL CHECK (event_type IN (
                       'payment.failed', 'payment.succeeded', 'checkout.abandoned',
                       'subscription.failed', 'mandate.failed', 'invoice.overdue',
                       'recovery.created', 'recovery.analyzed', 'recovery.authorized',
                       'recovery.blocked', 'recovery.escalated', 'recovery.attempted',
                       'recovery.succeeded', 'recovery.failed', 'recovery.unknown'
                   )),
    aggregate_type TEXT NOT NULL CHECK (aggregate_type <> ''),
    aggregate_id   UUID NOT NULL,
    merchant_id    UUID NOT NULL REFERENCES merchants(id),
    payload        JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at    TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (event_id)
);

CREATE INDEX idx_recovery_events_merchant_id ON recovery_events (merchant_id);
CREATE INDEX idx_recovery_events_aggregate ON recovery_events (aggregate_type, aggregate_id);
CREATE INDEX idx_recovery_events_event_type ON recovery_events (event_type);
