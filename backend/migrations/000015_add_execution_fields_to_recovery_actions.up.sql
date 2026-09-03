-- Milestone 6 (Execution Engine) needs recovery_actions to represent a
-- real execution attempt, not just a decided-but-unexecuted action:
--
-- 1. 'UNKNOWN' is a genuinely new, missing status: execution was
--    attempted but its outcome could not be definitively determined
--    (provider timeout, transport error, or an interrupted execution
--    discovered on retry). This is never fabricated into SUCCEEDED or
--    FAILED — only Milestone 7's webhook/reconciliation can resolve it.
--    Postgres does not support altering a CHECK constraint's condition
--    in place, so the existing constraint is dropped and recreated with
--    the additional value.
-- 2. provider/provider_reference/error_code/execution_metadata record
--    which PaymentProvider executed the action and what it reported —
--    sanitized, structured data only, never raw provider responses,
--    never secrets. See docs/architecture/execution-engine.md.
ALTER TABLE recovery_actions
    DROP CONSTRAINT recovery_actions_status_check;

ALTER TABLE recovery_actions
    ADD CONSTRAINT recovery_actions_status_check CHECK (status IN (
        'PENDING', 'EXECUTING', 'SUCCEEDED', 'FAILED', 'SKIPPED', 'UNKNOWN'
    ));

ALTER TABLE recovery_actions
    ADD COLUMN provider TEXT,
    ADD COLUMN provider_reference TEXT,
    ADD COLUMN error_code TEXT,
    ADD COLUMN execution_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- At most one RecoveryAction may claim a given provider reference: a
-- defensive guarantee against a bug ever recording the same external
-- side effect against two different action rows. NULL references
-- (nothing to claim yet, or execution never reached a definitive
-- success) are unconstrained.
CREATE UNIQUE INDEX idx_recovery_actions_provider_reference_unique
    ON recovery_actions (provider, provider_reference)
    WHERE provider_reference IS NOT NULL;
