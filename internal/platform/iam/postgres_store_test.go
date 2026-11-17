package iam

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yz1qaq/asterflow/internal/domain"
)

func TestPostgresUserStore(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	t.Run("empty list is an array", func(t *testing.T) {
		users, err := store.ListUsers(ctx, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(users)
		if err != nil || string(data) != "[]" {
			t.Fatalf("empty users = %s, error = %v", data, err)
		}
	})

	id := uuid.New()
	user := &domain.User{
		ID:    strings.ReplaceAll(id.String(), "-", ""),
		Email: "store@example.test", Name: "Store User", PasswordHash: "fixture-hash",
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}
	t.Run("create writes back database values and reads fresh objects", func(t *testing.T) {
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		if user.ID != id.String() || user.Role != GlobalRoleMember || user.Status != "active" {
			t.Fatalf("defaults or canonical UUID not written back: %+v", user)
		}
		if user.CreatedAt.Before(time.Now().Add(-time.Minute)) || !user.CreatedAt.Equal(user.UpdatedAt) {
			t.Fatal("timestamps must come from the database INSERT")
		}
		byEmail, err := store.GetUserByEmail(ctx, user.Email)
		if err != nil || !reflect.DeepEqual(byEmail, user) {
			t.Fatalf("email lookup mismatch: %v", err)
		}
		byEmail.Name = "caller mutation"
		byID, err := store.GetUserByID(ctx, user.ID)
		if err != nil || !reflect.DeepEqual(byID, user) {
			t.Fatalf("ID lookup must read persisted data: %v", err)
		}
		data, err := json.Marshal(byID)
		if err != nil || strings.Contains(string(data), "fixture-hash") ||
			strings.Contains(string(data), "password") {
			t.Fatal("public user JSON must exclude the password hash")
		}
	})

	t.Run("email conflict is distinct from primary key conflict", func(t *testing.T) {
		duplicate := *user
		duplicate.ID = uuid.NewString()
		before := duplicate
		if err := store.CreateUser(ctx, &duplicate); !errors.Is(err, ErrEmailExists) {
			t.Fatalf("duplicate email error = %v", err)
		}
		if !reflect.DeepEqual(duplicate, before) {
			t.Fatal("failed creation modified the caller's user")
		}
		collision := *user
		collision.Email = "another@example.test"
		err := store.CreateUser(ctx, &collision)
		var pgErr *pgconn.PgError
		if errors.Is(err, ErrEmailExists) || !errors.As(err, &pgErr) ||
			pgErr.Code != "23505" || pgErr.ConstraintName != "users_pkey" {
			t.Fatalf("primary key failure must preserve its database cause: %v", err)
		}
	})

	t.Run("missing users invalid IDs and cancellation", func(t *testing.T) {
		if _, err := store.GetUserByEmail(ctx, "missing@example.test"); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("missing email error = %v", err)
		}
		if _, err := store.GetUserByID(ctx, uuid.NewString()); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("missing ID error = %v", err)
		}
		if _, err := store.GetUserByID(ctx, "invalid"); err == nil {
			t.Fatal("invalid UUID accepted")
		}
		if err := store.SetUserRole(ctx, "invalid", GlobalRoleAdmin); err == nil {
			t.Fatal("invalid role target UUID accepted")
		}
		if err := store.CreateUser(ctx, nil); err == nil {
			t.Fatal("nil user accepted")
		}
		invalid := &domain.User{ID: "invalid", Email: "invalid@example.test"}
		before := *invalid
		if err := store.CreateUser(ctx, invalid); err == nil || !reflect.DeepEqual(*invalid, before) {
			t.Fatal("invalid creation must fail without modifying input")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := store.GetUserByEmail(canceled, user.Email); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	})

	t.Run("role changes persist and pagination applies", func(t *testing.T) {
		second := &domain.User{ID: uuid.NewString(), Email: "second@example.test",
			Role: GlobalRoleAdmin, Status: "inactive"}
		if err := store.CreateUser(ctx, second); err != nil {
			t.Fatal(err)
		}
		if second.Role != GlobalRoleAdmin || second.Status != "inactive" {
			t.Fatal("explicit role and status were overwritten")
		}
		if err := store.SetUserRole(ctx, user.ID, GlobalRoleAdmin); err != nil {
			t.Fatal(err)
		}
		updated, err := store.GetUserByID(ctx, user.ID)
		if err != nil || updated.Role != GlobalRoleAdmin || updated.UpdatedAt.Before(user.UpdatedAt) {
			t.Fatalf("role change did not persist: %v", err)
		}
		page, err := store.ListUsers(ctx, 1, 1)
		if err != nil || len(page) != 1 {
			t.Fatalf("pagination: count=%d, error=%v", len(page), err)
		}
		end, err := store.ListUsers(ctx, 1, 2)
		if err != nil || len(end) != 0 {
			t.Fatalf("past final page: count=%d, error=%v", len(end), err)
		}
	})
}

// Every connection uses a generated schema. No public tables are modified.
func newIAMTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for isolated PostgreSQL IAM tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("cannot connect to IAM test PostgreSQL; check explicit test URL")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_iam_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated IAM schema: %v", err)
		}
	})
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"000001_init_iam.up.sql", "000002_workspace_rbac.up.sql"} {
		migration, err := os.ReadFile("../../../migrations/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("initialize isolated IAM schema: %v", err)
		}
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid IAM test PostgreSQL URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create IAM test pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("ping IAM test pool")
	}
	return pool
}
