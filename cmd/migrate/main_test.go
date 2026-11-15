package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunArguments(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		value string
		want  string
	}{
		{"no_action", nil, "", "choose exactly one action"},
		{"up_and_version", []string{"-up", "-version"}, "", "choose exactly one action"},
		{"up_and_down", []string{"-up", "-down", "1"}, "", "choose exactly one action"},
		{"down_and_version", []string{"-down", "1", "-version"}, "", "choose exactly one action"},
		{"zero_down", []string{"-down", "0"}, "", "-down must be greater"},
		{"negative_down", []string{"-down", "-1"}, "", "-down must be greater"},
		{"zero_down_with_up", []string{"-up", "-down", "0"}, "", "-down must be greater"},
		{"invalid_integer", []string{"-down", "abc"}, "", "invalid value"},
		{"unknown_flag", []string{"-unknown"}, "", "flag provided but not defined"},
		{"positional_argument", []string{"-up", "unexpected"}, "", "unexpected positional"},
		{"empty_path", []string{"-up", "-path", " "}, "", "migration path is required"},
		{"empty_env_name", []string{"-up", "-database-env", " "}, "", "environment variable name is required"},
		{"missing_url", []string{"-up"}, "", "ASTERFLOW_DATABASE_URL is required"},
		{"blank_url", []string{"-up"}, " \t", "ASTERFLOW_DATABASE_URL is required"},
		{"malformed_url", []string{"-up"}, "postgres://unit:test-password-must-not-appear@localhost:invalid/unit", "invalid database URL"},
		{"malformed_query_escape", []string{"-up"}, "postgres://unit@127.0.0.1:1/unit?sslmode=disable&search_path=isolated%zz", "invalid database URL query"},
		{"malformed_query_separator", []string{"-up"}, "postgres://unit@127.0.0.1:1/unit?sslmode=disable&search_path=isolated;other=value", "invalid database URL query"},
		{"unsupported_scheme", []string{"-up"}, "https://localhost/unit", "must use postgres"},
		{"help", []string{"-h"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ASTERFLOW_DATABASE_URL", tc.value)
			err := run(tc.args)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if strings.Contains(err.Error(), "test-password-must-not-appear") {
				t.Fatal("argument error exposed the connection password")
			}
		})
	}
}

func TestMigrateCLI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "asterflow-migrate")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, "./cmd/migrate")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build migration executable: %v\n%s", err, output)
	}

	t.Run("help_exits_zero_without_database", func(t *testing.T) {
		out, code := migrationCommand(t, binary, root, nil, "-h")
		if code != 0 || !strings.Contains(out, "-database-env") {
			t.Fatalf("want help exit 0, got %d: %s", code, out)
		}
	})
	t.Run("invalid_invocation_exits_one", func(t *testing.T) {
		out, code := migrationCommand(t, binary, root, nil)
		if code != 1 || !strings.Contains(out, "choose exactly one action") {
			t.Fatalf("want invalid invocation exit 1, got %d: %s", code, out)
		}
	})
	t.Run("initialization_error_hides_password", func(t *testing.T) {
		const secret = "private-test-password"
		env := map[string]string{
			"ASTERFLOW_DATABASE_URL": "postgres://unit:" + secret + "@127.0.0.1:1/unit?connect_timeout=invalid",
		}
		out, code := migrationCommand(t, binary, root, env, "-version")
		if code != 1 || !strings.Contains(out, "initialize migrations") {
			t.Fatalf("want initialization failure, got %d", code)
		}
		if strings.Contains(out, secret) {
			t.Fatal("initialization error exposed credentials")
		}
	})

	t.Run("postgres_upgrade_constraints_rollback_rebuild", func(t *testing.T) {
		db := newMigrationDB(t)
		env := map[string]string{"ASTERFLOW_DATABASE_URL": db.url}
		runCLI := func(want string, args ...string) {
			t.Helper()
			out, code := migrationCommand(t, binary, root, env, args...)
			if code != 0 || !strings.Contains(out, want) {
				t.Fatalf("want %q and exit 0, got exit %d: %s", want, code, out)
			}
		}
		runCLI("version=none", "-version")
		runCLI("migrations applied", "-up")
		runCLI("version=1 dirty=false", "-version")
		assertMigrationTables(t, db, true)

		const userID = "11111111-1111-4111-8111-111111111111"
		const roleID = "22222222-2222-4222-8222-222222222222"
		const keyID = "33333333-3333-4333-8333-333333333333"
		db.exec(t, "INSERT INTO users(id,email) VALUES($1,'migration@example.test')", userID)
		db.exec(t, "INSERT INTO workspaces(id,name) VALUES('test-space','Test')")
		db.exec(t, "INSERT INTO workspace_members(workspace_id,user_id) VALUES('test-space',$1)", userID)
		db.exec(t, "INSERT INTO roles(id,name) VALUES($1,'reader')", roleID)
		db.exec(t, "INSERT INTO user_roles(user_id,role_id,workspace_id) VALUES($1,$2,'test-space')", userID, roleID)
		db.exec(t, "INSERT INTO role_permissions(role,resource,action) VALUES('reader','prompt','read')")
		db.exec(t, "INSERT INTO api_keys(id,user_id,hash) VALUES($1,$2,'test-key-hash')", keyID, userID)

		t.Run("defaults_and_text_workspace_id", func(t *testing.T) {
			var role, status, password string
			var hasTimes bool
			err := db.conn.QueryRow(db.ctx,
				"SELECT role,status,password_hash,created_at IS NOT NULL AND updated_at IS NOT NULL FROM users WHERE id=$1",
				userID).Scan(&role, &status, &password, &hasTimes)
			if err != nil || role != "member" || status != "active" || password != "" || !hasTimes {
				t.Fatalf("unexpected user defaults: %v", err)
			}
			var memberRole, workspaceType string
			if err := db.conn.QueryRow(db.ctx, "SELECT role FROM workspace_members WHERE workspace_id='test-space'").Scan(&memberRole); err != nil || memberRole != "member" {
				t.Fatalf("unexpected membership default: %v", err)
			}
			if err := db.conn.QueryRow(db.ctx,
				"SELECT data_type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='workspaces' AND column_name='id'").Scan(&workspaceType); err != nil || workspaceType != "text" {
				t.Fatalf("workspace ID must retain TEXT type: %v", err)
			}
		})
		t.Run("database_constraints", func(t *testing.T) {
			db.wantCode(t, "23505", "INSERT INTO users(id,email) VALUES('44444444-4444-4444-8444-444444444444','migration@example.test')")
			db.wantCode(t, "22P02", "INSERT INTO users(id,email) VALUES('not-a-uuid','invalid@example.test')")
			db.wantCode(t, "23502", "INSERT INTO users(id,email) VALUES('44444444-4444-4444-8444-444444444444',NULL)")
			db.wantCode(t, "23505", "INSERT INTO workspace_members(workspace_id,user_id) VALUES('test-space',$1)", userID)
			db.wantCode(t, "23503", "INSERT INTO workspace_members(workspace_id,user_id) VALUES('missing-space',$1)", userID)
			db.wantCode(t, "23505", "INSERT INTO user_roles(user_id,role_id,workspace_id) VALUES($1,$2,'test-space')", userID, roleID)
			db.wantCode(t, "23505", "INSERT INTO api_keys(id,user_id,hash) VALUES('44444444-4444-4444-8444-444444444444',$1,'test-key-hash')", userID)
		})
		t.Run("repeat_upgrade_preserves_data", func(t *testing.T) {
			runCLI("migrations are up to date", "-up")
			if got := db.count(t, "SELECT count(*) FROM users"); got != 1 {
				t.Fatalf("repeat upgrade changed users: %d", got)
			}
			if got := db.count(t, "SELECT count(*) FROM api_keys"); got != 1 {
				t.Fatalf("repeat upgrade changed API keys: %d", got)
			}
		})
		t.Run("foreign_key_deletion_semantics", func(t *testing.T) {
			db.exec(t, "INSERT INTO workspaces(id,name) VALUES('parent','Parent')")
			db.exec(t, "INSERT INTO workspaces(id,name,parent_id) VALUES('child','Child','parent')")
			db.exec(t, "DELETE FROM workspaces WHERE id='parent'")
			var detached bool
			if err := db.conn.QueryRow(db.ctx, "SELECT parent_id IS NULL FROM workspaces WHERE id='child'").Scan(&detached); err != nil || !detached {
				t.Fatalf("child workspace was not detached: %v", err)
			}
			db.exec(t, "DELETE FROM users WHERE id=$1", userID)
			for _, table := range []string{"workspace_members", "user_roles", "api_keys"} {
				if got := db.count(t, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()); got != 0 {
					t.Fatalf("user delete left %d rows in %s", got, table)
				}
			}
			if got := db.count(t, "SELECT count(*) FROM workspaces"); got != 2 {
				t.Fatalf("user delete removed workspaces: %d", got)
			}
		})
		runCLI("rolled back 1 migration(s)", "-down", "1")
		runCLI("version=none", "-version")
		assertMigrationTables(t, db, false)
		out, code := migrationCommand(t, binary, root, env, "-down", "1")
		if code != 1 || !strings.Contains(out, "roll back migrations") {
			t.Fatalf("empty-version fixed-step rollback must fail, got %d: %s", code, out)
		}
		runCLI("migrations applied", "-up")
		runCLI("version=1 dirty=false", "-version")
		assertMigrationTables(t, db, true)
	})

	t.Run("schemes_special_path_and_custom_environment", func(t *testing.T) {
		db := newMigrationDB(t)
		directory := filepath.Join(t.TempDir(), "migration files #1")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"000001_init_iam.up.sql", "000001_init_iam.down.sql"} {
			data, err := os.ReadFile(filepath.Join(root, "migrations", name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, scheme := range []string{"postgres", "postgresql", "pgx5"} {
			t.Run(scheme, func(t *testing.T) {
				u, err := url.Parse(db.url)
				if err != nil {
					t.Fatal("invalid isolated database URL")
				}
				u.Scheme = scheme
				env := map[string]string{
					"ASTERFLOW_DATABASE_URL":     "invalid-default-must-not-be-used",
					"ASTERFLOW_MIGRATE_TEST_URL": u.String(),
				}
				for _, args := range [][]string{{"-up"}, {"-version"}, {"-down", "1"}} {
					fullArgs := append([]string{"-database-env", "ASTERFLOW_MIGRATE_TEST_URL", "-path", directory}, args...)
					out, code := migrationCommand(t, binary, root, env, fullArgs...)
					if code != 0 {
						t.Fatalf("scheme %s command failed: %s", scheme, out)
					}
					if args[0] == "-version" && !strings.Contains(out, "version=1 dirty=false") {
						t.Fatalf("unexpected version: %s", out)
					}
				}
			})
		}
	})

	t.Run("failed_sql_stays_dirty_and_blocks_changes", func(t *testing.T) {
		db := newMigrationDB(t)
		directory := t.TempDir()
		files := map[string]string{
			"000001_broken.up.sql":   "THIS IS INVALID SQL;",
			"000001_broken.down.sql": "SELECT 1;",
		}
		for name, text := range files {
			if err := os.WriteFile(filepath.Join(directory, name), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
		}
		env := map[string]string{"ASTERFLOW_DATABASE_URL": db.url}
		out, code := migrationCommand(t, binary, root, env, "-path", directory, "-up")
		if code != 1 || !strings.Contains(out, "apply migrations") {
			t.Fatalf("broken SQL must fail: %d %s", code, out)
		}
		out, code = migrationCommand(t, binary, root, env, "-path", directory, "-version")
		if code != 0 || !strings.Contains(out, "version=1 dirty=true") {
			t.Fatalf("failed SQL lost dirty state: %d %s", code, out)
		}
		for _, action := range [][]string{{"-up"}, {"-down", "1"}} {
			args := append([]string{"-path", directory}, action...)
			out, code = migrationCommand(t, binary, root, env, args...)
			if code != 1 || !strings.Contains(out, "Dirty database version") {
				t.Fatalf("dirty state did not block action: %d %s", code, out)
			}
		}
		var dirty bool
		if err := db.conn.QueryRow(db.ctx, "SELECT dirty FROM schema_migrations").Scan(&dirty); err != nil || !dirty {
			t.Fatalf("command silently cleared dirty state: %v", err)
		}
	})
}

func migrationCommand(t *testing.T, binary, root string, overrides map[string]string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = root
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "ASTERFLOW_DATABASE_URL" || key == "ASTERFLOW_MIGRATE_TEST_URL" || key == "ASTERFLOW_TEST_DATABASE_URL" {
			continue
		}
		command.Env = append(command.Env, entry)
	}
	for key, value := range overrides {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("migration subprocess exceeded its deadline")
	}
	if err == nil {
		return string(output), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("start migration subprocess: %v", err)
	}
	return string(output), exit.ExitCode()
}

type migrationDB struct {
	conn *pgx.Conn
	ctx  context.Context
	url  string
}

func newMigrationDB(t *testing.T) *migrationDB {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for isolated-schema PostgreSQL migration verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("cannot connect to migration test PostgreSQL; check explicit test URL")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	schema := "asterflow_mig_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		conn.Close(ctx)
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated migration schema: %v", err)
		}
		if err := conn.Close(cleanupCtx); err != nil {
			t.Errorf("close migration fixture connection: %v", err)
		}
	})
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid test PostgreSQL URL")
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	return &migrationDB{conn: conn, ctx: ctx, url: u.String()}
}

func (db *migrationDB) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := db.conn.Exec(db.ctx, query, args...); err != nil {
		t.Fatalf("fixture statement failed: %v", err)
	}
}

func (db *migrationDB) wantCode(t *testing.T, code, query string, args ...any) {
	t.Helper()
	_, err := db.conn.Exec(db.ctx, query, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("want PostgreSQL code %s, got %v", code, err)
	}
}

func (db *migrationDB) count(t *testing.T, query string) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(db.ctx, query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertMigrationTables(t *testing.T, db *migrationDB, up bool) {
	t.Helper()
	expected := []string{"schema_migrations"}
	if up {
		expected = []string{"api_keys", "role_permissions", "roles", "schema_migrations", "user_roles", "users", "workspace_members", "workspaces"}
	}
	rows, err := db.conn.Query(db.ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_type='BASE TABLE' ORDER BY table_name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("want tables %v, got %v", expected, actual)
	}
	if up {
		for _, index := range []string{"workspace_members_user", "api_keys_user"} {
			var exists bool
			if err := db.conn.QueryRow(db.ctx, "SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND indexname=$1)", index).Scan(&exists); err != nil || !exists {
				t.Fatalf("missing migration index %s: %v", index, err)
			}
		}
	}
}
