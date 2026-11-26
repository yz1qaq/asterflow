CREATE TABLE inventory_balances (
    warehouse_id TEXT NOT NULL,
    sku TEXT NOT NULL,
    available INTEGER NOT NULL DEFAULT 0 CHECK (available >= 0),
    reserved INTEGER NOT NULL DEFAULT 0 CHECK (reserved >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (warehouse_id, sku)
);

CREATE INDEX inventory_balances_sku_idx
    ON inventory_balances (sku, warehouse_id);