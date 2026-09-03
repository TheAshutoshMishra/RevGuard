-- Stores immutable Policy Engine decisions (Milestone 5): ALLOW, BLOCK, or
-- ESCALATE for a specific (RecoveryDiagnosis, RecoveryEconomicEvaluation)
-- pair. A row here is never updated after creation.
--
-- UNIQUE(recovery_case_id, recovery_diagnosis_id,
-- recovery_economic_evaluation_id, policy_version) is the idempotency
-- guarantee: re-evaluating the exact same inputs under the same policy
-- version can never create a second decision. A new diagnosis, a new
-- economic evaluation, or a new policy version legitimately produces a
-- new, independent decision.
CREATE TABLE policy_decisions (
    id                            UUID PRIMARY KEY,
    recovery_case_id              UUID NOT NULL REFERENCES recovery_cases(id),
    recovery_diagnosis_id         UUID NOT NULL REFERENCES recovery_diagnoses(id),
    recovery_economic_evaluation_id UUID NOT NULL REFERENCES recovery_economic_evaluations(id),

    decision           TEXT NOT NULL CHECK (decision IN ('ALLOW', 'BLOCK', 'ESCALATE')),
    -- Only meaningful when decision = 'ALLOW': the recommendation the
    -- policy authorized to proceed to execution (a future milestone).
    -- NULL for BLOCK/ESCALATE, where nothing is authorized.
    authorized_action  TEXT CHECK (authorized_action IS NULL OR authorized_action IN (
                           'retry_payment', 'send_payment_link', 'request_payment_method_change',
                           'send_reminder', 'escalate_to_human', 'stop_recovery'
                       )),
    policy_version     TEXT NOT NULL,
    -- Every reason code that applied, not just the first/most severe —
    -- see docs/architecture/policy-engine.md. Validated against the
    -- PolicyReasonCode vocabulary in Go, same as recovery_diagnoses'
    -- risk_flags is not DB-CHECK-constrained element-wise.
    reason_codes       JSONB NOT NULL DEFAULT '[]'::jsonb,
    explanation        TEXT NOT NULL,

    evaluated_at       TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (recovery_case_id, recovery_diagnosis_id, recovery_economic_evaluation_id, policy_version)
);

CREATE INDEX idx_policy_decisions_recovery_case_id ON policy_decisions (recovery_case_id);
CREATE INDEX idx_policy_decisions_recovery_diagnosis_id ON policy_decisions (recovery_diagnosis_id);
CREATE INDEX idx_policy_decisions_recovery_economic_evaluation_id ON policy_decisions (recovery_economic_evaluation_id);
