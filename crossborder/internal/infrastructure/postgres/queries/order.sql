-- name: GetOrder :one
SELECT
    id,
    shop_id,
    market,
    currency,
    amount::double precision AS amount,
    status,
    fulfillment_warehouse,
    ship_by,
    cancellation_open
FROM orders
WHERE id = $1;

-- name: ListOrderItems :many
SELECT
    order_id,
    line_no,
    sku,
    quantity,
    price::double precision AS price
FROM order_items
WHERE order_id = $1
ORDER BY line_no;