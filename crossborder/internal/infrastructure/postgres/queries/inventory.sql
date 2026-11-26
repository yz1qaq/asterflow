-- name: ListInventoryBySKU :many
SELECT warehouse_id, sku, available, reserved
FROM inventory_balances
WHERE sku = $1
ORDER BY warehouse_id;