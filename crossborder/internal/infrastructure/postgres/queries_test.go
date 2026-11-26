package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgstore "github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres/sqlc"
)

func newOrderQueriesTest(t *testing.T) (context.Context, pgx.Tx, *pgstore.Queries) {
	t.Helper()
	rawURL := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set CROSSBORDER_TEST_DATABASE_URL for isolated real PostgreSQL query tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, err := pgx.Connect(ctx, rawURL)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Close(context.Background())
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = tx.Rollback(cleanupCtx)
		_ = conn.Close(cleanupCtx)
		cancel()
	})
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"crossborder_queries_" + hex.EncodeToString(nonce[:])}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema+", pg_catalog"); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000001_create_orders.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	return ctx, tx, pgstore.New(tx)
}

func newInventoryQueriesTest(t *testing.T) (context.Context, pgx.Tx, *pgstore.Queries) {
	t.Helper()
	ctx, tx, q := newOrderQueriesTest(t)
	up, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000002_create_inventory.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	return ctx, tx, q
}

func TestInventoryQueries(t *testing.T) {
	t.Run("returns only matching SKU balances in warehouse order", func(t *testing.T) {
		ctx, tx, q := newInventoryQueriesTest(t)
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_balances(warehouse_id,sku,available,reserved) VALUES
			('WH-US-LAX','SKU-BLACK-M-01',18,3),
			('WH-CN-SZ','OTHER-SKU',99,4),
			('WH-CN-SZ','SKU-BLACK-M-01',0,2)`); err != nil {
			t.Fatal(err)
		}
		rows, err := q.ListInventoryBySKU(ctx, "SKU-BLACK-M-01")
		if err != nil || len(rows) != 2 {
			t.Fatalf("balances=%+v err=%v", rows, err)
		}
		if rows[0].WarehouseID != "WH-CN-SZ" || rows[0].Sku != "SKU-BLACK-M-01" || rows[0].Available != 0 || rows[0].Reserved != 2 ||
			rows[1].WarehouseID != "WH-US-LAX" || rows[1].Sku != "SKU-BLACK-M-01" || rows[1].Available != 18 || rows[1].Reserved != 3 {
			t.Fatalf("wrong filter, order or balance fields: %+v", rows)
		}
	})
	t.Run("missing SKU is a nonnil empty list", func(t *testing.T) {
		ctx, _, q := newInventoryQueriesTest(t)
		rows, err := q.ListInventoryBySKU(ctx, "missing")
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("balances=%#v err=%v", rows, err)
		}
	})
	t.Run("cancellation cause is preserved", func(t *testing.T) {
		ctx, _, q := newInventoryQueriesTest(t)
		canceled, stop := context.WithCancel(ctx)
		stop()
		if _, err := q.ListInventoryBySKU(canceled, "any"); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation=%v", err)
		}
	})
	t.Run("defaults and independent warehouse SKU uniqueness are enforced", func(t *testing.T) {
		ctx, tx, q := newInventoryQueriesTest(t)
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_balances(warehouse_id,sku) VALUES ('WH-A','SKU-A'),('WH-B','SKU-A'),('WH-A','SKU-B')`); err != nil {
			t.Fatal(err)
		}
		rows, err := q.ListInventoryBySKU(ctx, "SKU-A")
		if err != nil || len(rows) != 2 || rows[0].Available != 0 || rows[0].Reserved != 0 {
			t.Fatalf("defaults=%+v err=%v", rows, err)
		}
		for _, tc := range []struct{ sql, code string }{
			{`INSERT INTO inventory_balances(warehouse_id,sku) VALUES ('WH-A','SKU-A')`, "23505"},
			{`INSERT INTO inventory_balances(warehouse_id,sku,available) VALUES ('WH-X','SKU-A',-1)`, "23514"},
			{`INSERT INTO inventory_balances(warehouse_id,sku,reserved) VALUES ('WH-X','SKU-A',-1)`, "23514"},
			{`INSERT INTO inventory_balances(warehouse_id,sku) VALUES (NULL,'SKU-A')`, "23502"},
			{`INSERT INTO inventory_balances(warehouse_id,sku) VALUES ('WH-X',NULL)`, "23502"},
		} {
			if _, err := tx.Exec(ctx, "SAVEPOINT invalid_inventory"); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, tc.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("invalid row error=%v want SQLSTATE %s", err, tc.code)
			}
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_inventory; RELEASE SAVEPOINT invalid_inventory"); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("inventory rollback preserves orders and permits rebuild", func(t *testing.T) {
		ctx, tx, q := newInventoryQueriesTest(t)
		if _, err := tx.Exec(ctx, `INSERT INTO orders(id,shop_id,market,currency,amount,fulfillment_warehouse,ship_by)
			VALUES ('keep-order','shop','US','USD',1,'WH-A',now())`); err != nil {
			t.Fatal(err)
		}
		down, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000002_create_inventory.down.sql"))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := tx.Exec(ctx, string(down)); err != nil {
				t.Fatal(err)
			}
		}
		var inventoryExists bool
		if err := tx.QueryRow(ctx, "SELECT to_regclass('inventory_balances') IS NOT NULL").Scan(&inventoryExists); err != nil || inventoryExists {
			t.Fatalf("inventory exists=%v err=%v", inventoryExists, err)
		}
		if row, err := q.GetOrder(ctx, "keep-order"); err != nil || row.ID != "keep-order" {
			t.Fatalf("order was damaged: %+v err=%v", row, err)
		}
		up, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000002_create_inventory.up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
		if rows, err := q.ListInventoryBySKU(ctx, "any"); err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("rebuilt balances=%#v err=%v", rows, err)
		}
	})
}

func TestOrderQueries(t *testing.T) {
	t.Run("reads order fields and items in line order without mixing orders", func(t *testing.T) {
		ctx, tx, q := newOrderQueriesTest(t)
		shipBy := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
		if _, err := tx.Exec(ctx, `INSERT INTO orders
			(id,shop_id,market,currency,amount,status,fulfillment_warehouse,ship_by,cancellation_open)
			VALUES ('order-a','shop-us','US','USD',150.99,'partially_shipped','WH-CN-SZ',$1,false),
			('order-b','shop-other','US','USD',9.99,'awaiting_shipment','WH-US-LAX',$1,true)`, shipBy); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_items(order_id,line_no,sku,quantity,price) VALUES
			('order-a',2,'SKU-SECOND',2,10.50),
			('order-b',1,'SKU-OTHER',1,9.99),
			('order-a',1,'SKU-BLACK-M-01',1,129.99)`); err != nil {
			t.Fatal(err)
		}
		order, err := q.GetOrder(ctx, "order-a")
		if err != nil || order.ID != "order-a" || order.ShopID != "shop-us" || order.Market != "US" ||
			order.Currency != "USD" || order.Amount != 150.99 || order.Status != "partially_shipped" ||
			order.FulfillmentWarehouse != "WH-CN-SZ" || !order.ShipBy.Equal(shipBy) || order.CancellationOpen {
			t.Fatalf("order=%+v err=%v", order, err)
		}
		items, err := q.ListOrderItems(ctx, "order-a")
		if err != nil || len(items) != 2 {
			t.Fatalf("items=%+v err=%v", items, err)
		}
		if items[0].OrderID != "order-a" || items[0].LineNo != 1 || items[0].Sku != "SKU-BLACK-M-01" ||
			items[0].Quantity != 1 || items[0].Price != 129.99 ||
			items[1].LineNo != 2 || items[1].Sku != "SKU-SECOND" || items[1].Quantity != 2 || items[1].Price != 10.50 {
			t.Fatalf("items not correctly filtered, ordered or converted: %+v", items)
		}
	})
	t.Run("missing order preserves ErrNoRows and empty items return a nonnil slice", func(t *testing.T) {
		ctx, _, q := newOrderQueriesTest(t)
		if _, err := q.GetOrder(ctx, "missing"); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("missing order error=%v", err)
		}
		items, err := q.ListOrderItems(ctx, "missing")
		if err != nil || items == nil || len(items) != 0 {
			t.Fatalf("empty items=%#v err=%v", items, err)
		}
	})
	t.Run("canceled requests retain cancellation errors", func(t *testing.T) {
		ctx, _, q := newOrderQueriesTest(t)
		canceled, stop := context.WithCancel(ctx)
		stop()
		if _, err := q.GetOrder(canceled, "any"); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetOrder cancellation=%v", err)
		}
		if _, err := q.ListOrderItems(canceled, "any"); !errors.Is(err, context.Canceled) {
			t.Fatalf("ListOrderItems cancellation=%v", err)
		}
	})
}

func TestOpen(t *testing.T) {
	t.Run("opens a usable pool for the selected business database", func(t *testing.T) {
		rawURL := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
		if rawURL == "" {
			t.Skip("set CROSSBORDER_TEST_DATABASE_URL for real PostgreSQL")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pool, err := Open(ctx, rawURL)
		if err != nil || pool == nil {
			t.Fatalf("open: pool=%v err=%v", pool, err)
		}
		defer pool.Close()
		if err := pool.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		var database string
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil || database != pool.Config().ConnConfig.Database {
			t.Fatalf("connected database=%q err=%v", database, err)
		}
	})
	t.Run("invalid connection config returns no pool", func(t *testing.T) {
		pool, err := Open(context.Background(), "postgres://%zz")
		if pool != nil {
			pool.Close()
			t.Fatal("invalid connection returned a pool")
		}
		if err == nil {
			t.Fatal("invalid connection config accepted")
		}
	})
	t.Run("canceled startup preserves the cause and returns no pool", func(t *testing.T) {
		rawURL := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
		if rawURL == "" {
			t.Skip("set CROSSBORDER_TEST_DATABASE_URL for real PostgreSQL")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pool, err := Open(ctx, rawURL)
		if pool != nil {
			pool.Close()
			t.Fatal("canceled startup returned a pool")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause=%v", err)
		}
	})
}
