ALTER TABLE recovery_outcomes
    DROP CONSTRAINT IF EXISTS recovery_outcomes_recovery_action_id_unique;

ALTER TABLE recovery_outcomes
    DROP CONSTRAINT IF EXISTS recovery_outcomes_success_requires_amount;

ALTER TABLE recovery_outcomes
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS provider_webhook_event_id,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS provider;
