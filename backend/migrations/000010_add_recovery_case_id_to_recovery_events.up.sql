-- Links a recovery_events row to the RecoveryCase it was correlated to
-- (Milestone 2). Nullable: not every ingested event qualifies for case
-- creation (e.g. payment.succeeded), and events processed before this
-- column existed have no value to backfill.
ALTER TABLE recovery_events
    ADD COLUMN recovery_case_id UUID REFERENCES recovery_cases(id);

CREATE INDEX idx_recovery_events_recovery_case_id ON recovery_events (recovery_case_id);
