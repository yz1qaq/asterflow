BEGIN;

INSERT INTO orders (
    id, shop_id, market, currency, amount, status,
    fulfillment_warehouse, ship_by, cancellation_open
) VALUES (
    'TTS-20260801-1001', 'SHOP-US-001', 'US', 'USD', 129.99,
    'awaiting_shipment', 'WH-CN-SZ', now() + INTERVAL '8 hours', true
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO order_items (
    order_id, line_no, sku, quantity, price
) VALUES (
    'TTS-20260801-1001', 1, 'SKU-BLACK-M-01', 1, 129.99
)
ON CONFLICT (order_id, line_no) DO NOTHING;


INSERT INTO inventory_balances (
    warehouse_id, sku, available, reserved
) VALUES
    ('WH-CN-SZ', 'SKU-BLACK-M-01', 0, 0),
    ('WH-US-LAX', 'SKU-BLACK-M-01', 18, 0)
ON CONFLICT (warehouse_id, sku) DO NOTHING;

COMMIT;