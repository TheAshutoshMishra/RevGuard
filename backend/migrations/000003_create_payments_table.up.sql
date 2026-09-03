-- Monetary amounts are stored as integer minor units (e.g. paise, cents),
-- never as FLOAT/DOUBLE, to avoid decimal representation error. E.g.
-- INR 499.50 is stored as amount_minor_units = 49950, currency = 'INR'.
CREATE TABLE payments (
    id                   UUID PRIMARY KEY,
    merchant_id          UUID NOT NULL REFERENCES merchants(id),
    customer_id          UUID NOT NULL REFERENCES customers(id),
    external_payment_id  TEXT NOT NULL CHECK (external_payment_id <> ''),
    amount_minor_units   BIGINT NOT NULL CHECK (amount_minor_units >= 0),
    currency             CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status               TEXT NOT NULL CHECK (status IN (
                             'PENDING', 'SUCCEEDED', 'FAILED', 'REFUNDED', 'CANCELLED'
                         )),
    payment_method       TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (merchant_id, external_payment_id)
);

CREATE INDEX idx_payments_merchant_id ON payments (merchant_id);
CREATE INDEX idx_payments_customer_id ON payments (customer_id);
CREATE INDEX idx_payments_status ON payments (status);
