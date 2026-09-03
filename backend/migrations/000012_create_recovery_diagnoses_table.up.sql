-- Stores AI-generated diagnoses/recommendations for a RecoveryCase
-- (Milestone 3). This table is intentionally separate from
-- recovery_actions: a RecoveryDiagnosis is a recommendation, never an
-- authorized or executed action. A case may accumulate more than one row
-- here over time (e.g. re-analysis after a prior AI failure); rows are
-- immutable and never updated in place.
CREATE TABLE recovery_diagnoses (
    id                    UUID PRIMARY KEY,
    recovery_case_id      UUID NOT NULL REFERENCES recovery_cases(id),

    failure_category      TEXT NOT NULL CHECK (failure_category IN (
                              'transient_failure', 'insufficient_funds', 'payment_method_issue',
                              'authentication_issue', 'mandate_issue', 'customer_abandonment', 'unknown'
                          )),
    diagnosis_reason      TEXT NOT NULL,
    customer_context      TEXT NOT NULL,
    recommended_strategy  TEXT NOT NULL,

    recommended_action    TEXT NOT NULL CHECK (recommended_action IN (
                              'retry_payment', 'send_payment_link', 'request_payment_method_change',
                              'send_reminder', 'escalate_to_human', 'stop_recovery'
                          )),
    recommendation_reason TEXT NOT NULL,
    confidence            DOUBLE PRECISION NOT NULL CHECK (confidence >= 0.0 AND confidence <= 1.0),

    risk_flags            JSONB NOT NULL DEFAULT '[]'::jsonb,
    explanation           TEXT NOT NULL,

    -- Versioning metadata for reproducibility.
    provider              TEXT NOT NULL,
    model                 TEXT NOT NULL,
    prompt_version        TEXT NOT NULL,
    generated_at           TIMESTAMPTZ NOT NULL,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_recovery_diagnoses_recovery_case_id ON recovery_diagnoses (recovery_case_id);
