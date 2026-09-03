-- At most one non-CLOSED RecoveryCase may exist per payment. This is the
-- database-level guarantee that concurrent processing of qualifying
-- revenue-risk events for the same payment cannot create duplicate
-- recovery cases: a second concurrent INSERT will fail with a unique
-- violation (SQLSTATE 23505) and the caller falls back to reading the
-- case the winning transaction created. See
-- docs/architecture/event-flow.md.
CREATE UNIQUE INDEX idx_recovery_cases_open_payment_unique
    ON recovery_cases (payment_id)
    WHERE status <> 'CLOSED';
