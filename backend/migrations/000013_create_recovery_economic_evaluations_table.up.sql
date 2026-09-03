-- Stores deterministic economic evaluations of a RecoveryDiagnosis's
-- recommendation (Milestone 4). Separate from recovery_diagnoses (an AI
-- recommendation) and recovery_actions (an authorized/executed action):
-- this table records whether the recommendation had positive expected
-- economic value, and nothing here authorizes or executes anything.
--
-- UNIQUE(recovery_diagnosis_id) is the idempotency guarantee: at most one
-- economic evaluation exists per diagnosis. A new diagnosis (e.g. from
-- re-analysis) may get its own, separate evaluation row.
CREATE TABLE recovery_economic_evaluations (
    id                    UUID PRIMARY KEY,
    recovery_case_id      UUID NOT NULL REFERENCES recovery_cases(id),
    recovery_diagnosis_id UUID NOT NULL REFERENCES recovery_diagnoses(id),

    recommended_action    TEXT NOT NULL CHECK (recommended_action IN (
                              'retry_payment', 'send_payment_link', 'request_payment_method_change',
                              'send_reminder', 'escalate_to_human', 'stop_recovery'
                          )),

    revenue_at_risk_minor_units           BIGINT NOT NULL CHECK (revenue_at_risk_minor_units >= 0),
    recovery_probability_bps              INTEGER NOT NULL CHECK (recovery_probability_bps >= 0 AND recovery_probability_bps <= 10000),
    expected_gross_recovery_minor_units    BIGINT NOT NULL CHECK (expected_gross_recovery_minor_units >= 0),
    action_cost_minor_units               BIGINT NOT NULL CHECK (action_cost_minor_units >= 0),
    risk_cost_minor_units                 BIGINT NOT NULL CHECK (risk_cost_minor_units >= 0),
    -- Signed: expected gross recovery minus costs may be negative.
    expected_incremental_value_minor_units BIGINT NOT NULL,

    currency              CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),

    estimator_name        TEXT NOT NULL,
    estimator_version     TEXT NOT NULL,
    economic_model_version TEXT NOT NULL,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (recovery_diagnosis_id)
);

CREATE INDEX idx_recovery_economic_evaluations_recovery_case_id ON recovery_economic_evaluations (recovery_case_id);
CREATE INDEX idx_recovery_economic_evaluations_recovery_diagnosis_id ON recovery_economic_evaluations (recovery_diagnosis_id);
