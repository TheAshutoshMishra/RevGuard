CREATE TABLE recovery_actions (
    id                UUID PRIMARY KEY,
    recovery_case_id  UUID NOT NULL REFERENCES recovery_cases(id),
    action_type       TEXT NOT NULL CHECK (action_type IN (
                          'RETRY_PAYMENT', 'SEND_PAYMENT_LINK',
                          'REQUEST_PAYMENT_METHOD_CHANGE', 'SEND_REMINDER',
                          'ESCALATE_TO_HUMAN', 'STOP_RECOVERY'
                      )),
    status            TEXT NOT NULL CHECK (status IN (
                          'PENDING', 'EXECUTING', 'SUCCEEDED', 'FAILED', 'SKIPPED'
                      )),
    attempt_number    INT NOT NULL CHECK (attempt_number > 0),
    idempotency_key   TEXT NOT NULL,
    requested_at      TIMESTAMPTZ NOT NULL,
    executed_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (idempotency_key)
);

CREATE INDEX idx_recovery_actions_recovery_case_id ON recovery_actions (recovery_case_id);
