-- 0001: core procurement tables + scorecard snapshots.
-- Every table carries tenant_id so one database can host several customers.

CREATE TABLE IF NOT EXISTS suppliers (
    tenant_id  text NOT NULL,
    id         text NOT NULL,
    name       text NOT NULL,
    country    text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS purchase_orders (
    tenant_id       text NOT NULL,
    id              text NOT NULL,
    number          text NOT NULL,
    supplier_id     text NOT NULL,
    order_date      timestamptz NOT NULL,
    sent_at         timestamptz,
    acknowledged_at timestamptz,
    payment_terms   text NOT NULL DEFAULT '',
    incoterm        text NOT NULL DEFAULT '',
    currency        text NOT NULL DEFAULT 'USD',
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, supplier_id) REFERENCES suppliers (tenant_id, id)
);
CREATE INDEX IF NOT EXISTS po_order_date_idx ON purchase_orders (tenant_id, order_date);
CREATE UNIQUE INDEX IF NOT EXISTS po_number_idx ON purchase_orders (tenant_id, number);

CREATE TABLE IF NOT EXISTS po_lines (
    tenant_id     text NOT NULL,
    id            text NOT NULL,
    po_id         text NOT NULL,
    line_no       int  NOT NULL,
    part_number   text NOT NULL,
    category      text NOT NULL DEFAULT '',
    description   text NOT NULL DEFAULT '',
    qty           numeric NOT NULL,
    unit_price    numeric NOT NULL,
    promised_date timestamptz,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, po_id) REFERENCES purchase_orders (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS po_lines_part_idx ON po_lines (tenant_id, part_number);

CREATE TABLE IF NOT EXISTS receipts (
    tenant_id    text NOT NULL,
    line_id      text NOT NULL,
    seq          int  NOT NULL,
    received_on  timestamptz NOT NULL,
    qty_received numeric NOT NULL,
    qty_rejected numeric NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, line_id, seq),
    FOREIGN KEY (tenant_id, line_id) REFERENCES po_lines (tenant_id, id) ON DELETE CASCADE
);

-- Immutable scorecard snapshots (history of how a supplier was rated).
CREATE TABLE IF NOT EXISTS scorecards (
    tenant_id   text NOT NULL,
    supplier_id text NOT NULL,
    as_of       timestamptz NOT NULL,
    overall     numeric,
    grade       text NOT NULL,
    body        jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, supplier_id, as_of)
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
