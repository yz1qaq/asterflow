package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/yz1qaq/asterflow/crossborder/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool, q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{
		db: db,
		q:  q,
	}
}

func orderFromRows(row pgstore.GetOrderRow, items []pgstore.ListOrderItemsRow) domain.Order {
	orderItems := make([]domain.OrderItem, 0, len(items))

	for _, item := range items {
		orderItems = append(orderItems, domain.OrderItem{
			SKU:      item.Sku,
			Quantity: int(item.Quantity),
			Price:    item.Price,
		})
	}

	return domain.Order{
		ID:               row.ID,
		ShopID:           row.ShopID,
		Market:           row.Market,
		Currency:         row.Currency,
		Amount:           row.Amount,
		Status:           row.Status,
		FulfillmentWH:    row.FulfillmentWarehouse,
		ShipBy:           row.ShipBy,
		Items:            orderItems,
		CancellationOpen: row.CancellationOpen,
	}
}

func (s *PostgresStore) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return domain.Order{}, fmt.Errorf("begin order query: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	q := s.q.WithTx(tx)

	row, err := q.GetOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, fmt.Errorf(
			"%w: order %q",
			ErrNotFound,
			id,
		)
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("get order: %w", err)
	}

	items, err := q.ListOrderItems(ctx, id)
	if err != nil {
		return domain.Order{}, fmt.Errorf("list order items: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, fmt.Errorf("commit order query: %w", err)
	}

	return orderFromRows(row, items), nil
}

func (s *PostgresStore) ListInventoryBySKU(ctx context.Context, sku string) ([]domain.InventoryBalance, error) {
	rows, err := s.q.ListInventoryBySKU(ctx, sku)
	if err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}

	balances := make([]domain.InventoryBalance, 0, len(rows))
	for _, row := range rows {
		balances = append(balances, domain.InventoryBalance{
			WarehouseID: row.WarehouseID,
			SKU:         row.Sku,
			Available:   int(row.Available),
			Reserved:    int(row.Reserved),
		})
	}

	return balances, nil
}
