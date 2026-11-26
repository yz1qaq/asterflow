package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yz1qaq/asterflow/crossborder/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/crossborder/internal/infrastructure/postgres/sqlc"

	"github.com/jackc/pgx/v5"
)

func TestSimulatorProcess(t *testing.T) {
	if os.Getenv("CROSSBORDER_SIMULATOR_HELPER") != "1" {
		return
	}
	main()
}

func simulatorCommand(t *testing.T, dsn string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestSimulatorProcess$")
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "CROSSBORDER_SIMULATOR_HELPER", "CROSSBORDER_DATABASE_URL", "CROSSBORDER_ADDR", "KBOT_DATABASE_URL":
		default:
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "CROSSBORDER_SIMULATOR_HELPER=1", "CROSSBORDER_ADDR=127.0.0.1:0", "KBOT_DATABASE_URL=postgres://wrong-platform-db")
	if dsn != "" {
		cmd.Env = append(cmd.Env, "CROSSBORDER_DATABASE_URL="+dsn)
	}
	return cmd
}

func TestSimulatorRequiresBusinessConnection(t *testing.T) {
	cmd := simulatorCommand(t, "")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "CROSSBORDER_DATABASE_URL is required") {
		t.Fatalf("missing business connection must fail; err=%v output=%s", err, out)
	}
}

func startSimulator(t *testing.T, dsn string) (string, func()) {
	t.Helper()
	cmd := simulatorCommand(t, dsn)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			const marker = "crossborder listening on "
			if _, address, ok := strings.Cut(line, marker); ok {
				select {
				case ready <- strings.TrimSpace(address):
				default:
				}
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	t.Cleanup(func() {
		if exited {
			return
		}
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("simulator process did not exit")
		}
	})
	var baseURL string
	select {
	case baseURL = <-ready:
	case err := <-done:
		exited = true
		t.Fatalf("simulator exited before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("simulator did not announce its listening address")
	}
	stop := func() {
		if exited {
			return
		}
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			exited = true
			if err != nil {
				t.Fatalf("graceful shutdown failed: %v", err)
			}
		case <-time.After(12 * time.Second):
			t.Fatal("graceful shutdown timed out")
		}
	}
	return baseURL, stop
}

func TestSimulatorPostgresLifecycle(t *testing.T) {
	dsn := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CROSSBORDER_TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
	schema := "crossborder_startup_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
		}
	}()
	testURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	params := testURL.Query()
	params.Set("search_path", schema+",pg_catalog")
	testURL.RawQuery = params.Encode()
	testDSN := testURL.String()
	cfg, err := pgx.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close(context.Background())
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations=%v err=%v", files, err)
	}
	for _, name := range files {
		sql, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	seed, err := os.ReadFile(filepath.Join("..", "..", "seed", "demo.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// Force the last insert to fail and verify the complete seed is atomic.
	if _, err := fixture.Exec(ctx, `ALTER TABLE inventory_balances
		ADD CONSTRAINT reject_seed_test CHECK (warehouse_id <> 'WH-US-LAX')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, string(seed)); err == nil {
		t.Fatal("seed must fail when an inventory row is rejected")
	}
	if _, err := fixture.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	var seededRows int
	if err := fixture.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM orders) + (SELECT count(*) FROM order_items) +
		(SELECT count(*) FROM inventory_balances)`).Scan(&seededRows); err != nil || seededRows != 0 {
		t.Fatalf("failed seed left partial business data: rows=%d err=%v", seededRows, err)
	}
	if _, err := fixture.Exec(ctx, "ALTER TABLE inventory_balances DROP CONSTRAINT reject_seed_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	q := pgstore.New(fixture)
	balances, err := q.ListInventoryBySKU(ctx, "SKU-BLACK-M-01")
	if err != nil || len(balances) != 2 || balances[0].WarehouseID != "WH-CN-SZ" ||
		balances[0].Available != 0 || balances[0].Reserved != 0 ||
		balances[1].WarehouseID != "WH-US-LAX" || balances[1].Available != 18 || balances[1].Reserved != 0 {
		t.Fatalf("seeded inventory=%+v err=%v", balances, err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	query := func(baseURL string) domain.Order {
		t.Helper()
		res, err := client.Post(baseURL+"/tools/get_order", "application/json", strings.NewReader(`{"order_id":"TTS-20260801-1001"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var order domain.Order
		if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&order) != nil {
			t.Fatalf("order response status=%d", res.StatusCode)
		}
		return order
	}
	queryInventory := func(baseURL string) []domain.InventoryBalance {
		t.Helper()
		res, err := client.Post(baseURL+"/tools/get_inventory", "application/json", strings.NewReader(`{"sku":"SKU-BLACK-M-01"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct {
			SKU      string                    `json:"sku"`
			Balances []domain.InventoryBalance `json:"balances"`
		}
		if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&out) != nil || out.SKU != "SKU-BLACK-M-01" {
			t.Fatalf("inventory HTTP status=%d body=%+v", res.StatusCode, out)
		}
		return out.Balances
	}
	baseURL, stop := startSimulator(t, testDSN)
	res, err := client.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health status=%d", res.StatusCode)
	}
	order := query(baseURL)
	if order.ID != "TTS-20260801-1001" || order.Amount != 129.99 || order.Status != "awaiting_shipment" ||
		order.FulfillmentWH != "WH-CN-SZ" || len(order.Items) != 1 || order.Items[0].SKU != "SKU-BLACK-M-01" || order.Items[0].Quantity != 1 || !order.ShipBy.After(time.Now()) {
		t.Fatalf("seeded order=%+v", order)
	}
	inventory := queryInventory(baseURL)
	if len(inventory) != 2 || inventory[0].WarehouseID != "WH-CN-SZ" || inventory[0].Available != 0 ||
		inventory[1].WarehouseID != "WH-US-LAX" || inventory[1].Available != 18 || inventory[1].Reserved != 0 {
		t.Fatalf("initial inventory HTTP data=%+v", inventory)
	}
	stop()
	if res, err := client.Get(baseURL + "/healthz"); err == nil {
		res.Body.Close()
		t.Fatal("HTTP port still responds after process exit")
	}
	if _, err := fixture.Exec(ctx, `UPDATE orders SET amount=99,status='partially_shipped' WHERE id='TTS-20260801-1001';
		UPDATE order_items SET quantity=3 WHERE order_id='TTS-20260801-1001' AND line_no=1;
		UPDATE inventory_balances SET available=7,reserved=2 WHERE warehouse_id='WH-US-LAX' AND sku='SKU-BLACK-M-01';`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	balances, err = q.ListInventoryBySKU(ctx, "SKU-BLACK-M-01")
	if err != nil || len(balances) != 2 || balances[1].WarehouseID != "WH-US-LAX" ||
		balances[1].Available != 7 || balances[1].Reserved != 2 {
		t.Fatalf("repeat seed overwrote or duplicated inventory: %+v err=%v", balances, err)
	}
	baseURL2, stop2 := startSimulator(t, testDSN)
	order = query(baseURL2)
	if order.Amount != 99 || order.Status != "partially_shipped" || len(order.Items) != 1 || order.Items[0].Quantity != 3 {
		t.Fatalf("repeat seed or restart overwrote persisted values: %+v", order)
	}
	inventory = queryInventory(baseURL2)
	if len(inventory) != 2 || inventory[1].WarehouseID != "WH-US-LAX" ||
		inventory[1].Available != 7 || inventory[1].Reserved != 2 {
		t.Fatalf("persisted inventory HTTP data=%+v", inventory)
	}
	stop2()
	var orders, items int
	if err := fixture.QueryRow(ctx, "SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM order_items)").Scan(&orders, &items); err != nil || orders != 1 || items != 1 {
		t.Fatalf("seed duplicated rows: orders=%d items=%d err=%v", orders, items, err)
	}
}
