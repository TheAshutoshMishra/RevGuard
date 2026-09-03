CREATE TABLE customers (
    id                    UUID PRIMARY KEY,
    merchant_id           UUID NOT NULL REFERENCES merchants(id),
    external_customer_id  TEXT NOT NULL CHECK (external_customer_id <> ''),
    email                 TEXT,
    name                  TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (merchant_id, external_customer_id)
);

CREATE INDEX idx_customers_merchant_id ON customers (merchant_id);
