DROP INDEX IF EXISTS idx_recovery_events_recovery_case_id;
ALTER TABLE recovery_events DROP COLUMN IF EXISTS recovery_case_id;
