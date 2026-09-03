CREATE TABLE recovery_cases (
    id                        UUID PRIMARY KEY,
    merchant_id               UUID NOT NULL REFERENCES merchants(id),
    customer_id               UUID NOT NULL REFERENCES customers(id),
    payment_id                UUID NOT NULL REFERENCES payments(id),
    status                    TEXT NOT NULL CHECK (status IN (
                                  'DETECTED', 'ANALYZING', 'ANALYZED', 'POLICY_CHECK',
                                  'ALLOW', 'BLOCK', 'ESCALATE', 'EXECUTING', 'VERIFYING',
                                  'SUCCESS', 'FAILED', 'UNKNOWN', 'CLOSED'
                              )),
    revenue_at_risk_minor_units BIGINT NOT NULL CHECK (revenue_at_risk_minor_units >= 0),
    currency                  CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at                 TIMESTAMPTZ
);

CREATE INDEX idx_recovery_cases_merchant_id ON recovery_cases (merchant_id);
CREATE INDEX idx_recovery_cases_customer_id ON recovery_cases (customer_id);
CREATE INDEX idx_recovery_cases_payment_id ON recovery_cases (payment_id);
CREATE INDEX idx_recovery_cases_status ON recovery_cases (status);
