CREATE TABLE audit_events (
    id                UUID PRIMARY KEY,
    recovery_case_id  UUID NOT NULL REFERENCES recovery_cases(id),
    event_type        TEXT NOT NULL CHECK (event_type <> ''),
    actor_type        TEXT NOT NULL CHECK (actor_type IN (
                          'SYSTEM', 'AI', 'POLICY_ENGINE', 'HUMAN', 'WEBHOOK'
                      )),
    actor_id          TEXT,
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_events_recovery_case_id ON audit_events (recovery_case_id);
