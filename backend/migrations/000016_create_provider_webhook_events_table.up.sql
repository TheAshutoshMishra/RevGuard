CREATE TABLE provider_webhook_events (
    id                  UUID PRIMARY KEY,
    provider            TEXT NOT NULL,
    provider_event_id   TEXT NOT NULL,
    event_type          TEXT NOT NULL,
    provider_reference  TEXT,
    status              TEXT NOT NULL CHECK (status IN ('CAPTURED', 'FAILED', 'PENDING')),
    amount_minor_units  BIGINT NOT NULL DEFAULT 0 CHECK (amount_minor_units >= 0),
    currency            CHAR(3) CHECK (currency IS NULL OR currency ~ '^[A-Z]{3}$'),
    occurred_at         TIMESTAMPTZ NOT NULL,
    recovery_action_id  UUID REFERENCES recovery_actions(id),
    matched             BOOLEAN NOT NULL DEFAULT false,
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    received_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_event_id)
);

CREATE INDEX idx_provider_webhook_events_recovery_action_id ON provider_webhook_events (recovery_action_id);
CREATE INDEX idx_provider_webhook_events_provider_reference ON provider_webhook_events (provider, provider_reference);
