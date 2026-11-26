package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yz1qaq/asterflow/crossborder/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Each Store test commits fixtures only inside its own disposable schema.
func newPostgresStoreTest(t *testing.T) (context.Context, *pgxpool.Pool, *PostgresStore) {
	t.Helper()
	dsn := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CROSSBORDER_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "crossborder_store_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove test schema: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",pg_catalog"
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_create_orders.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, NewPostgresStore(pool, pgstore.New(pool))
}

func seedOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO orders
		(id,shop_id,market,currency,amount,status,fulfillment_warehouse,ship_by,cancellation_open)
		VALUES ('order-a','shop-us','US','USD',150.99,'partially_shipped','WH-CN-SZ',
		'2026-11-07T08:00:00Z',false),
		('order-b','shop-other','CA','CAD',1,'awaiting_shipment','WH-CA',
		'2026-11-08T08:00:00Z',true);
		INSERT INTO order_items(order_id,line_no,sku,quantity,price) VALUES
		('order-a',2,'SKU-SECOND',2,10.50),
		('order-b',1,'SKU-OTHER',1,1),
		('order-a',1,'SKU-BLACK-M-01',1,129.99)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStoreGetOrder(t *testing.T) {
	t.Run("assembles complete order with only its own sorted items", func(t *testing.T) {
		ctx, pool, store := newPostgresStoreTest(t)
		seedOrder(t, ctx, pool)
		got, err := store.GetOrder(ctx, "order-a")
		want := domain.Order{
			ID: "order-a", ShopID: "shop-us", Market: "US", Currency: "USD",
			Amount: 150.99, Status: "partially_shipped", FulfillmentWH: "WH-CN-SZ",
			ShipBy: time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC),
			Items: []domain.OrderItem{
				{SKU: "SKU-BLACK-M-01", Quantity: 1, Price: 129.99},
				{SKU: "SKU-SECOND", Quantity: 2, Price: 10.50},
			},
			CancellationOpen: false,
		}
		// Equal compares the instant independent of the driver's time location.
		if !got.ShipBy.Equal(want.ShipBy) {
			t.Fatalf("ship_by=%v want %v", got.ShipBy, want.ShipBy)
		}
		got.ShipBy = got.ShipBy.UTC()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("GetOrder=%+v err=%v want=%+v", got, err, want)
		}
	})
	t.Run("existing order without items has an empty nonnil list", func(t *testing.T) {
		ctx, pool, store := newPostgresStoreTest(t)
		seedOrder(t, ctx, pool)
		if _, err := pool.Exec(ctx, "DELETE FROM order_items WHERE order_id='order-a'"); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetOrder(ctx, "order-a")
		if err != nil || got.ID != "order-a" || got.Items == nil || len(got.Items) != 0 {
			t.Fatalf("GetOrder=%+v err=%v", got, err)
		}
	})
	t.Run("missing order becomes a business not-found error", func(t *testing.T) {
		ctx, _, store := newPostgresStoreTest(t)
		got, err := store.GetOrder(ctx, "missing")
		if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(got, domain.Order{}) {
			t.Fatalf("GetOrder=%+v err=%v", got, err)
		}
	})
	t.Run("canceled request retains cancellation", func(t *testing.T) {
		ctx, _, store := newPostgresStoreTest(t)
		canceled, stop := context.WithCancel(ctx)
		stop()
		got, err := store.GetOrder(canceled, "order-a")
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, domain.Order{}) {
			t.Fatalf("GetOrder=%+v err=%v", got, err)
		}
	})
	t.Run("item query failure never returns a partial order", func(t *testing.T) {
		ctx, pool, store := newPostgresStoreTest(t)
		seedOrder(t, ctx, pool)
		if _, err := pool.Exec(ctx, "DROP TABLE order_items"); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetOrder(ctx, "order-a")
		if err == nil || !reflect.DeepEqual(got, domain.Order{}) {
			t.Fatalf("GetOrder=%+v err=%v", got, err)
		}
		if err := pool.Ping(ctx); err != nil {
			t.Fatalf("connection not reusable after failure: %v", err)
		}
	})
	t.Run("concurrent update cannot mix old header with new items", func(t *testing.T) {
		ctx, writer, _ := newPostgresStoreTest(t)
		seedOrder(t, ctx, writer)
		barrier := &orderReadBarrier{read: make(chan struct{}), resume: make(chan struct{})}
		defer close(barrier.resume)
		cfg := writer.Config()
		cfg.ConnConfig.Tracer = barrier
		reader, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reader.Close)
		store := NewPostgresStore(reader, pgstore.New(reader))
		type result struct {
			order domain.Order
			err   error
		}
		done := make(chan result, 1)
		go func() {
			order, err := store.GetOrder(ctx, "order-a")
			done <- result{order, err}
		}()
		select {
		case <-barrier.read:
		case <-ctx.Done():
			t.Fatal("header query did not complete before timeout")
		}
		// Both changes are committed after the reader has read the header.
		if _, err := writer.Exec(ctx, `BEGIN;
			UPDATE orders SET amount=280.98 WHERE id='order-a';
			UPDATE order_items SET quantity=2 WHERE order_id='order-a' AND line_no=1;
			COMMIT`); err != nil {
			t.Fatal(err)
		}
		barrier.resume <- struct{}{}
		select {
		case got := <-done:
			if got.err != nil || got.order.Amount != 150.99 || len(got.order.Items) != 2 ||
				got.order.Items[0].Quantity != 1 {
				t.Fatalf("mixed database snapshots: order=%+v err=%v", got.order, got.err)
			}
		case <-ctx.Done():
			t.Fatal("GetOrder did not complete before timeout")
		}
	})
}

type orderReadBarrierKey struct{}

// Pause between the two real queries without adding hooks to production code.
type orderReadBarrier struct {
	read   chan struct{}
	resume chan struct{}
}

func (b *orderReadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: GetOrder :one") {
		return context.WithValue(ctx, orderReadBarrierKey{}, true)
	}
	return ctx
}

func (b *orderReadBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(orderReadBarrierKey{}) != true || data.Err != nil {
		return
	}
	close(b.read)
	select {
	case <-b.resume:
	case <-ctx.Done():
	}
}

type orderStoreFunc func(context.Context, string) (domain.Order, error)

func (f orderStoreFunc) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	return f(ctx, id)
}

func (f orderStoreFunc) ListInventoryBySKU(context.Context, string) ([]domain.InventoryBalance, error) {
	return nil, errors.New("unexpected inventory query in order test")
}

func TestServiceGetOrder(t *testing.T) {
	t.Run("rejects blank IDs before querying storage", func(t *testing.T) {
		store := orderStoreFunc(func(context.Context, string) (domain.Order, error) {
			t.Fatal("blank ID must not reach storage")
			return domain.Order{}, nil
		})
		svc := NewService(store)
		for _, id := range []string{"", " \t\n"} {
			got, err := svc.GetOrder(context.Background(), id)
			if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(got, domain.Order{}) {
				t.Fatalf("id=%q order=%+v err=%v", id, got, err)
			}
		}
	})
	t.Run("trims whitespace while retaining ID case and request context", func(t *testing.T) {
		type requestKey struct{}
		ctx := context.WithValue(context.Background(), requestKey{}, "request-1")
		store := orderStoreFunc(func(gotCtx context.Context, id string) (domain.Order, error) {
			if gotCtx != ctx || id != "Order-A" {
				t.Fatalf("query ctx=%v id=%q", gotCtx, id)
			}
			return domain.Order{ID: "Order-A", Amount: 129.99,
				Items: []domain.OrderItem{{SKU: "SKU-BLACK-M-01", Quantity: 1, Price: 129.99}}}, nil
		})
		got, err := NewService(store).GetOrder(ctx, " \tOrder-A\n")
		if err != nil || got.ID != "Order-A" || got.Amount != 129.99 || len(got.Items) != 1 ||
			got.Items[0].SKU != "SKU-BLACK-M-01" {
			t.Fatalf("GetOrder=%+v err=%v", got, err)
		}
	})
	t.Run("retains not found, cancellation and other storage errors", func(t *testing.T) {
		unavailable := errors.New("storage unavailable")
		for _, cause := range []error{ErrNotFound, context.Canceled, unavailable} {
			store := orderStoreFunc(func(context.Context, string) (domain.Order, error) {
				return domain.Order{}, cause
			})
			got, err := NewService(store).GetOrder(context.Background(), "order-a")
			if !errors.Is(err, cause) || !reflect.DeepEqual(got, domain.Order{}) {
				t.Fatalf("cause=%v order=%+v err=%v", cause, got, err)
			}
		}
	})
	t.Run("uses the real PostgreSQL store for success and business not found", func(t *testing.T) {
		ctx, pool, store := newPostgresStoreTest(t)
		seedOrder(t, ctx, pool)
		svc := NewService(store)
		got, err := svc.GetOrder(ctx, " order-a ")
		if err != nil || got.ID != "order-a" || got.Amount != 150.99 || len(got.Items) != 2 {
			t.Fatalf("GetOrder=%+v err=%v", got, err)
		}
		if _, err := svc.GetOrder(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("business not found error=%v", err)
		}
		if _, err := svc.GetOrder(ctx, " "); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid input error=%v", err)
		}
	})
}

func newInventoryStoreTest(t *testing.T) (context.Context, *pgxpool.Pool, *PostgresStore) {
	t.Helper()
	ctx, pool, store := newPostgresStoreTest(t)
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000002_create_inventory.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, store
}

func TestPostgresStoreInventory(t *testing.T) {
	t.Run("maps and filters ordered warehouse balances", func(t *testing.T) {
		ctx, pool, store := newInventoryStoreTest(t)
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_balances(warehouse_id,sku,available,reserved) VALUES
			('WH-US-LAX','SKU-A',18,3),('WH-CN-SZ','OTHER',99,4),('WH-CN-SZ','SKU-A',0,2)`); err != nil {
			t.Fatal(err)
		}
		got, err := store.ListInventoryBySKU(ctx, "SKU-A")
		want := []domain.InventoryBalance{
			{WarehouseID: "WH-CN-SZ", SKU: "SKU-A", Available: 0, Reserved: 2},
			{WarehouseID: "WH-US-LAX", SKU: "SKU-A", Available: 18, Reserved: 3},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("inventory=%+v err=%v want=%+v", got, err, want)
		}
	})
	t.Run("missing SKU returns an empty nonnil list", func(t *testing.T) {
		ctx, _, store := newInventoryStoreTest(t)
		got, err := store.ListInventoryBySKU(ctx, "missing")
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("inventory=%#v err=%v", got, err)
		}
	})
	t.Run("cancellation and PostgreSQL error causes are retained", func(t *testing.T) {
		ctx, pool, store := newInventoryStoreTest(t)
		canceled, stop := context.WithCancel(ctx)
		stop()
		if _, err := store.ListInventoryBySKU(canceled, "SKU-A"); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled inventory=%v", err)
		}
		if _, err := pool.Exec(ctx, "DROP TABLE inventory_balances"); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ListInventoryBySKU(ctx, "SKU-A")
		var pgErr *pgconn.PgError
		if rows != nil || !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
			t.Fatalf("inventory=%+v error=%v", rows, err)
		}
	})
}

type inventoryStoreFunc func(context.Context, string) ([]domain.InventoryBalance, error)

func (f inventoryStoreFunc) GetOrder(context.Context, string) (domain.Order, error) {
	return domain.Order{}, errors.New("unexpected order query in inventory test")
}

func (f inventoryStoreFunc) ListInventoryBySKU(ctx context.Context, sku string) ([]domain.InventoryBalance, error) {
	return f(ctx, sku)
}

func TestServiceInventory(t *testing.T) {
	t.Run("blank SKU is rejected before storage", func(t *testing.T) {
		store := inventoryStoreFunc(func(context.Context, string) ([]domain.InventoryBalance, error) {
			t.Fatal("blank SKU reached storage")
			return nil, nil
		})
		for _, sku := range []string{"", " \t\n"} {
			if _, err := NewService(store).Inventory(context.Background(), sku); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("sku=%q err=%v", sku, err)
			}
		}
	})
	t.Run("normalizes SKU and forwards context and balances", func(t *testing.T) {
		type requestKey struct{}
		ctx := context.WithValue(context.Background(), requestKey{}, "request-1")
		store := inventoryStoreFunc(func(gotCtx context.Context, sku string) ([]domain.InventoryBalance, error) {
			if gotCtx != ctx || sku != "SKU-a" {
				t.Fatalf("ctx=%v sku=%q", gotCtx, sku)
			}
			return []domain.InventoryBalance{{WarehouseID: "WH-A", SKU: "SKU-a", Available: 18, Reserved: 3}}, nil
		})
		rows, err := NewService(store).Inventory(ctx, " \tSKU-a\n")
		if err != nil || len(rows) != 1 || rows[0].WarehouseID != "WH-A" || rows[0].Available != 18 || rows[0].Reserved != 3 {
			t.Fatalf("balances=%+v err=%v", rows, err)
		}
	})
	t.Run("storage error causes are preserved", func(t *testing.T) {
		unavailable := errors.New("storage unavailable")
		for _, cause := range []error{context.Canceled, unavailable} {
			store := inventoryStoreFunc(func(context.Context, string) ([]domain.InventoryBalance, error) { return nil, cause })
			if _, err := NewService(store).Inventory(context.Background(), "SKU-A"); !errors.Is(err, cause) {
				t.Fatalf("cause=%v err=%v", cause, err)
			}
		}
	})
}
