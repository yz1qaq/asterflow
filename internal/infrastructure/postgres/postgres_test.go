package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenRejectsInvalidConfiguration(t *testing.T) {
	const secret = "test-password-must-not-appear"
	pool, err := Open(context.Background(),
		"postgres://user:"+secret+"@127.0.0.1:invalid/asterflow")
	if pool != nil {
		pool.Close()
		t.Fatal("invalid configuration returned a pool")
	}
	if err == nil {
		t.Fatal("invalid configuration was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("configuration error exposed the connection password")
	}
}

func TestOpenHonorsContext(t *testing.T) {
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"canceled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, context.Canceled},
		{"expired", func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}, context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()
			pool, err := Open(ctx, "postgres://unused:unused@127.0.0.1:1/asterflow?sslmode=disable")
			if pool != nil {
				pool.Close()
				t.Fatal("failed initialization returned a pool")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("want context error %v, got %v", tc.want, err)
			}
		})
	}
}

func TestOpenWithPostgres(t *testing.T) {
	databaseURL := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for real PostgreSQL verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := Open(ctx, databaseURL)
	if err != nil {
		// Do not print credentials from an externally supplied connection string.
		t.Fatal("cannot open integration PostgreSQL; check the isolated test environment")
	}
	t.Cleanup(pool.Close)

	t.Run("query_after_open_returns", func(t *testing.T) {
		var answer int
		if err := pool.QueryRow(ctx, "SELECT 42").Scan(&answer); err != nil {
			t.Fatalf("query on returned pool failed: %v", err)
		}
		if answer != 42 {
			t.Fatalf("want 42, got %d", answer)
		}
	})

	t.Run("initialization_context_does_not_own_pool", func(t *testing.T) {
		initCtx, stopInit := context.WithCancel(ctx)
		anotherPool, err := Open(initCtx, databaseURL)
		stopInit()
		if err != nil {
			t.Fatal("cannot open pool with initialization context")
		}
		defer anotherPool.Close()
		if err := anotherPool.Ping(ctx); err != nil {
			t.Fatalf("canceling initialization context closed the pool: %v", err)
		}
		anotherPool.Close()
		if err := anotherPool.Ping(ctx); err == nil {
			t.Fatal("closed pool still accepted Ping")
		}
	})
}
