package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestMigrationCLIProcess(t *testing.T) {
	if os.Getenv("CROSSBORDER_MIGRATE_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"migrate"}, os.Args[i+1:]...)
			flag.CommandLine = flag.NewFlagSet("migrate", flag.ExitOnError)
			main()
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func runMigrationCLI(ctx context.Context, connection string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestMigrationCLIProcess$", "--"}, args...)...)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "CROSSBORDER_DATABASE_URL=") ||
			strings.HasPrefix(entry, "CROSSBORDER_MIGRATE_TEST_PROCESS=") ||
			strings.HasPrefix(entry, "KBOT_DATABASE_URL=") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "CROSSBORDER_MIGRATE_TEST_PROCESS=1", "KBOT_DATABASE_URL=not-a-database-url")
	if connection != "" {
		cmd.Env = append(cmd.Env, "CROSSBORDER_DATABASE_URL="+connection)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestMigrationRequiresBusinessConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := runMigrationCLI(ctx, "", "-version")
	if err == nil || !strings.Contains(output, "缺少环境变量：CROSSBORDER_DATABASE_URL") {
		t.Fatalf("must require the business connection instead of reading KBOT_DATABASE_URL: err=%v output=%s", err, output)
	}
}

func TestOrderMigrationCLI(t *testing.T) {
	rawURL := os.Getenv("CROSSBORDER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set CROSSBORDER_TEST_DATABASE_URL for isolated real PostgreSQL migration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	database := "crossborder_migrate_test_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{database}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("cleanup disposable database: %v", err)
		}
	}()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + database
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	// Freeze this test at revision 1 so later inventory migrations do not
	// change the initial order migration's up/down contract.
	path := t.TempDir()
	for _, name := range []string{"000001_create_orders.up.sql", "000001_create_orders.down.sql"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) string {
		t.Helper()
		output, err := runMigrationCLI(ctx, u.String(), append([]string{"-path", path}, args...)...)
		if err != nil {
			t.Fatalf("migration %v: %v\n%s", args, err, output)
		}
		return output
	}
	assertState := func(wantTables, wantVersions int) {
		t.Helper()
		var tables, versions int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename IN ('orders','order_items')").Scan(&tables); err != nil || tables != wantTables {
			t.Fatalf("business tables=%d want=%d err=%v", tables, wantTables, err)
		}
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != wantVersions {
			t.Fatalf("migration records=%d want=%d err=%v", versions, wantVersions, err)
		}
		if wantVersions > 0 {
			var version int64
			var dirty bool
			if err := conn.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 1 || dirty {
				t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
			}
		}
	}
	run("-version")
	assertState(0, 0)
	run("-up")
	assertState(2, 1)
	if !strings.Contains(run("-version"), "dirty=false") {
		t.Fatal("missing clean migration state")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO orders(id,shop_id,market,currency,amount,fulfillment_warehouse,ship_by)
		VALUES ('order-test','shop','US','USD',129.99,'WH-CN-SZ',now());
		INSERT INTO order_items(order_id,line_no,sku,quantity,price) VALUES ('order-test',1,'SKU',1,129.99);
		CREATE TABLE unrelated(id int);`); err != nil {
		t.Fatal(err)
	}
	run("-up")
	assertState(2, 1)
	var quantity int
	if err := conn.QueryRow(ctx, "SELECT quantity FROM order_items WHERE order_id='order-test' AND line_no=1").Scan(&quantity); err != nil || quantity != 1 {
		t.Fatal("repeated up changed existing data", quantity, err)
	}
	run("-down", "1")
	assertState(0, 0)
	var kept bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('public.unrelated') IS NOT NULL").Scan(&kept); err != nil || !kept {
		t.Fatal("down removed unrelated data", err)
	}
	run("-version")
	run("-up")
	assertState(2, 1)
}
