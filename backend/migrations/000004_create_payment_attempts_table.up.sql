CREATE TABLE payment_attempts (
    id              UUID PRIMARY KEY,
    payment_id      UUID NOT NULL REFERENCES payments(id),
    attempt_number  INT NOT NULL CHECK (attempt_number > 0),
    status          TEXT NOT NULL CHECK (status IN ('PENDING', 'SUCCEEDED', 'FAILED')),
    failure_code    TEXT,
    failure_reason  TEXT,
    started_at      TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (payment_id, attempt_number)
);

CREATE INDEX idx_payment_attempts_payment_id ON payment_attempts (payment_id);
