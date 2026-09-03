DROP INDEX IF EXISTS idx_recovery_actions_provider_reference_unique;

ALTER TABLE recovery_actions
    DROP COLUMN IF EXISTS provider,
    DROP COLUMN IF EXISTS provider_reference,
    DROP COLUMN IF EXISTS error_code,
    DROP COLUMN IF EXISTS execution_metadata;

ALTER TABLE recovery_actions
    DROP CONSTRAINT recovery_actions_status_check;

ALTER TABLE recovery_actions
    ADD CONSTRAINT recovery_actions_status_check CHECK (status IN (
        'PENDING', 'EXECUTING', 'SUCCEEDED', 'FAILED', 'SKIPPED'
    ));
