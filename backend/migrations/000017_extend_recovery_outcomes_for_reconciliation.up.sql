ALTER TABLE recovery_outcomes
    ADD COLUMN provider TEXT,
    ADD COLUMN source TEXT CHECK (source IN ('WEBHOOK', 'RECONCILIATION')),
    ADD COLUMN provider_webhook_event_id UUID REFERENCES provider_webhook_events(id),
    ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Financial safety: a SUCCESS outcome must always carry a positive
-- provider-confirmed recovered amount. Never allow a SUCCESS row that
-- silently means "we recovered nothing."
ALTER TABLE recovery_outcomes
    ADD CONSTRAINT recovery_outcomes_success_requires_amount
    CHECK (status <> 'SUCCESS' OR recovered_amount_minor_units > 0);

-- At most one durable financial outcome per execution attempt. The
-- guarded RecoveryCase.Status transition (VERIFYING -> SUCCESS/FAILED,
-- which can only ever succeed once per case) is what makes this true
-- under concurrency; this constraint is the database-level backstop.
ALTER TABLE recovery_outcomes
    ADD CONSTRAINT recovery_outcomes_recovery_action_id_unique UNIQUE (recovery_action_id);
