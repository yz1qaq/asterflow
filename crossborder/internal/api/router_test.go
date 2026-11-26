package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yz1qaq/asterflow/crossborder/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres/sqlc"
	"github.com/yz1qaq/asterflow/crossborder/internal/service"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type orderStoreFunc func(context.Context, string) (domain.Order, error)

func (f orderStoreFunc) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	return f(ctx, id)
}

func (f orderStoreFunc) ListInventoryBySKU(context.Context, string) ([]domain.InventoryBalance, error) {
	return nil, errors.New("unexpected inventory query in order test")
}

type inventoryStoreFunc func(context.Context, string) ([]domain.InventoryBalance, error)

func (f inventoryStoreFunc) GetOrder(context.Context, string) (domain.Order, error) {
	return domain.Order{}, errors.New("unexpected order query in inventory test")
}

func (f inventoryStoreFunc) ListInventoryBySKU(ctx context.Context, sku string) ([]domain.InventoryBalance, error) {
	return f(ctx, sku)
}

func TestInventoryRoutes(t *testing.T) {
	t.Run("returns normalized SKU and warehouse balances using request context", func(t *testing.T) {
		type requestKey struct{}
		ctx := context.WithValue(context.Background(), requestKey{}, "request-1")
		store := inventoryStoreFunc(func(gotCtx context.Context, sku string) ([]domain.InventoryBalance, error) {
			if gotCtx != ctx || sku != "SKU-A" {
				t.Fatalf("ctx=%v sku=%q", gotCtx, sku)
			}
			return []domain.InventoryBalance{{WarehouseID: "WH-A", SKU: "SKU-A", Available: 18, Reserved: 3}}, nil
		})
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/tools/get_inventory", strings.NewReader(`{"sku":" SKU-A "}`)).WithContext(ctx)
		New(service.NewService(store)).ServeHTTP(w, r)
		var out struct {
			SKU      string                    `json:"sku"`
			Balances []domain.InventoryBalance `json:"balances"`
		}
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || json.Unmarshal(w.Body.Bytes(), &out) != nil ||
			out.SKU != "SKU-A" || len(out.Balances) != 1 || out.Balances[0].WarehouseID != "WH-A" ||
			out.Balances[0].SKU != "SKU-A" || out.Balances[0].Available != 18 || out.Balances[0].Reserved != 3 {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("unknown SKU is a successful empty array", func(t *testing.T) {
		store := inventoryStoreFunc(func(context.Context, string) ([]domain.InventoryBalance, error) {
			return []domain.InventoryBalance{}, nil
		})
		w := httptest.NewRecorder()
		New(service.NewService(store)).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tools/get_inventory", strings.NewReader(`{"sku":"missing"}`)))
		var out struct {
			SKU      string                    `json:"sku"`
			Balances []domain.InventoryBalance `json:"balances"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.SKU != "missing" || out.Balances == nil || len(out.Balances) != 0 {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("missing, blank and malformed SKU input is rejected", func(t *testing.T) {
		store := inventoryStoreFunc(func(context.Context, string) ([]domain.InventoryBalance, error) {
			t.Fatal("invalid input reached storage")
			return nil, nil
		})
		h := New(service.NewService(store))
		for _, body := range []string{`{}`, `{"sku":" "}`, `{"sku":12}`, `{"sku":"SKU-A","unknown":true}`, `{"sku":"SKU-A"}{}`} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tools/get_inventory", strings.NewReader(body)))
			if w.Code != 400 {
				t.Fatalf("body=%s response=%d %s", body, w.Code, w.Body.String())
			}
		}
	})
	t.Run("storage errors reach the shared internal error response", func(t *testing.T) {
		store := inventoryStoreFunc(func(context.Context, string) ([]domain.InventoryBalance, error) {
			return nil, errors.New("private database details")
		})
		w := httptest.NewRecorder()
		New(service.NewService(store)).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tools/get_inventory", strings.NewReader(`{"sku":"SKU-A"}`)))
		var out map[string]string
		if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["error"] != "internal server error" {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	})
}

func TestOrderRoutes(t *testing.T) {
	t.Run("order response uses request context and domain JSON", func(t *testing.T) {
		type requestKey struct{}
		ctx := context.WithValue(context.Background(), requestKey{}, "request-1")
		store := orderStoreFunc(func(gotCtx context.Context, id string) (domain.Order, error) {
			if gotCtx != ctx || id != "order-a" {
				t.Fatalf("context or ID changed: ctx=%v id=%q", gotCtx, id)
			}
			return domain.Order{ID: "order-a", ShopID: "shop-us", Amount: 129.99,
				Items: []domain.OrderItem{{SKU: "SKU-BLACK-M-01", Quantity: 1, Price: 129.99}}}, nil
		})
		r := httptest.NewRequest(http.MethodPost, "/tools/get_order", strings.NewReader(`{"order_id":" order-a "}`)).WithContext(ctx)
		w := httptest.NewRecorder()
		New(service.NewService(store)).ServeHTTP(w, r)
		var got domain.Order
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" ||
			json.Unmarshal(w.Body.Bytes(), &got) != nil || got.ID != "order-a" || got.ShopID != "shop-us" ||
			got.Amount != 129.99 || len(got.Items) != 1 || got.Items[0].SKU != "SKU-BLACK-M-01" {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("invalid input is rejected before storage", func(t *testing.T) {
		store := orderStoreFunc(func(context.Context, string) (domain.Order, error) {
			t.Fatal("invalid request reached storage")
			return domain.Order{}, nil
		})
		h := New(service.NewService(store))
		cases := []struct{ name, body string }{
			{"empty", ""}, {"malformed", "{"}, {"missing ID", `{}`},
			{"blank ID", `{"order_id":" \t "}`}, {"null", `null`},
			{"wrong type", `{"order_id":12}`},
			{"unknown field", `{"order_id":"order-a","unexpected":true}`},
			{"second JSON value", `{"order_id":"order-a"}{}`},
			{"trailing garbage", `{"order_id":"order-a"} broken`},
			{"over body limit", `{"order_id":"` + strings.Repeat("a", (1<<20)+1) + `"}`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tools/get_order", strings.NewReader(tc.body)))
				var out map[string]string
				if w.Code != http.StatusBadRequest || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["error"] == "" {
					t.Fatalf("response=%d %s", w.Code, w.Body.String())
				}
			})
		}
	})
	t.Run("business errors map to HTTP status without leaking internal errors", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			err     error
			status  int
			message string
		}{
			{"not found", fmt.Errorf("%w: order missing", service.ErrNotFound), 404, "business resource not found: order missing"},
			{"invalid input", fmt.Errorf("%w: order rejected", service.ErrInvalidInput), 400, "invalid business input: order rejected"},
			{"internal failure", errors.New("db host=private password=SECRET"), 500, "internal server error"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				store := orderStoreFunc(func(context.Context, string) (domain.Order, error) { return domain.Order{}, tc.err })
				w := httptest.NewRecorder()
				New(service.NewService(store)).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tools/get_order", strings.NewReader(`{"order_id":"order-a"}`)))
				var out map[string]string
				if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["error"] != tc.message {
					t.Fatalf("response=%d %s", w.Code, w.Body.String())
				}
			})
		}
	})
	t.Run("health is independent of business reads", func(t *testing.T) {
		store := orderStoreFunc(func(context.Context, string) (domain.Order, error) {
			t.Fatal("health called business storage")
			return domain.Order{}, nil
		})
		w := httptest.NewRecorder()
		New(service.NewService(store)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		var got map[string]string
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil ||
			got["status"] != "ok" || got["project"] != "crossborder" {
			t.Fatalf("response=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("order route requires POST and unknown paths are not found", func(t *testing.T) {
		store := orderStoreFunc(func(context.Context, string) (domain.Order, error) {
			t.Fatal("unmatched route called storage")
			return domain.Order{}, nil
		})
		h := New(service.NewService(store))
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{http.MethodGet, "/tools/get_order", 405},
			{http.MethodPost, "/unknown", 404},
		} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("%s %s status=%d want %d", tc.method, tc.path, w.Code, tc.status)
			}
		}
	})
}

func TestOrderHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CROSSBORDER_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "crossborder_http_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",pg_catalog"
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_create_orders.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO orders(id,shop_id,market,currency,amount,fulfillment_warehouse,ship_by)
		VALUES ('TTS-20260801-1001','SHOP-US-001','US','USD',129.99,'WH-CN-SZ','2026-11-07T08:00:00Z');
		INSERT INTO order_items(order_id,line_no,sku,quantity,price)
		VALUES ('TTS-20260801-1001',1,'SKU-BLACK-M-01',1,129.99)`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(service.NewService(service.NewPostgresStore(pool, pgstore.New(pool)))))
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	for _, tc := range []struct {
		id     string
		status int
	}{
		{" TTS-20260801-1001 ", 200}, {"missing", 404}, {" ", 400},
	} {
		body, err := json.Marshal(map[string]string{"order_id": tc.id})
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Post(server.URL+"/tools/get_order", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != tc.status {
			res.Body.Close()
			t.Fatalf("ID=%q status=%d want %d", tc.id, res.StatusCode, tc.status)
		}
		if tc.status == 200 {
			var order domain.Order
			err = json.NewDecoder(res.Body).Decode(&order)
			if err != nil || order.ID != "TTS-20260801-1001" || order.Amount != 129.99 ||
				order.FulfillmentWH != "WH-CN-SZ" || len(order.Items) != 1 || order.Items[0].Quantity != 1 {
				res.Body.Close()
				t.Fatalf("order=%+v err=%v", order, err)
			}
		}
		res.Body.Close()
	}
}
