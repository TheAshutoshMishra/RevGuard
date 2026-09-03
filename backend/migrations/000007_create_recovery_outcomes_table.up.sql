CREATE TABLE recovery_outcomes (
    id                          UUID PRIMARY KEY,
    recovery_case_id            UUID NOT NULL REFERENCES recovery_cases(id),
    recovery_action_id          UUID NOT NULL REFERENCES recovery_actions(id),
    status                      TEXT NOT NULL CHECK (status IN ('SUCCESS', 'FAILED', 'UNKNOWN')),
    recovered_amount_minor_units BIGINT NOT NULL CHECK (recovered_amount_minor_units >= 0),
    currency                    CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    external_reference          TEXT,
    observed_at                 TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_recovery_outcomes_recovery_case_id ON recovery_outcomes (recovery_case_id);
CREATE INDEX idx_recovery_outcomes_recovery_action_id ON recovery_outcomes (recovery_action_id);
