CREATE TABLE orders (
    id TEXT PRIMARY KEY,
    shop_id TEXT NOT NULL,
    market TEXT NOT NULL,
    currency TEXT NOT NULL,

    amount NUMERIC(18, 2) NOT NULL CHECK (amount >= 0),

    status TEXT NOT NULL DEFAULT 'awaiting_shipment'
        CHECK (
            status IN (
                'awaiting_shipment',
                'partially_shipped',
                'cancelled'
            )
        ),

    fulfillment_warehouse TEXT NOT NULL,
    ship_by TIMESTAMPTZ NOT NULL,
    cancellation_open BOOLEAN NOT NULL DEFAULT true,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE order_items (
    order_id TEXT NOT NULL
        REFERENCES orders(id) ON DELETE CASCADE,

    line_no INTEGER NOT NULL CHECK (line_no > 0),
    sku TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    price NUMERIC(18, 2) NOT NULL CHECK (price >= 0),

    PRIMARY KEY (order_id, line_no)
);