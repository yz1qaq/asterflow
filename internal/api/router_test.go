package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type readinessCheckFunc func(context.Context) error

func (f readinessCheckFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestNewRouter(t *testing.T) {
	handler := NewRouter(readinessCheckFunc(func(context.Context) error { return nil }), nil)
	if handler == nil {
		t.Fatal("NewRouter returned nil")
	}
	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"health", http.MethodGet, "/healthz", http.StatusOK},
		{"wrong_method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{"unknown", http.MethodGet, "/missing", http.StatusNotFound},
		{"not_a_prefix", http.MethodGet, "/healthz/extra", http.StatusNotFound},
		{"trailing_slash", http.MethodGet, "/healthz/", http.StatusNotFound},
		{"ready", http.MethodGet, "/readyz", http.StatusOK},
		{"ready_wrong_method", http.MethodPost, "/readyz", http.StatusMethodNotAllowed},
		{"ready_trailing_slash", http.MethodGet, "/readyz/", http.StatusNotFound},
		{"ready_not_a_prefix", http.MethodGet, "/readyz/extra", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("want status %d, got %d", tc.wantStatus, recorder.Code)
			}
			if tc.wantStatus == http.StatusOK {
				assertPlainResponse(t, recorder, http.StatusOK, "ok")
			}
		})
	}
}

func TestReadiness(t *testing.T) {
	t.Run("missing_checker", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		NewRouter(nil, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
	})
	t.Run("dependency_error_is_not_exposed", func(t *testing.T) {
		calls := 0
		handler := NewRouter(readinessCheckFunc(func(context.Context) error {
			calls++
			return errors.New("private database error must not be exposed")
		}), nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
		if calls != 1 {
			t.Fatalf("want one dependency check, got %d", calls)
		}
	})
	t.Run("health_does_not_probe_database", func(t *testing.T) {
		handler := NewRouter(readinessCheckFunc(func(context.Context) error {
			t.Fatal("healthz attempted to check the database")
			return nil
		}), nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		assertPlainResponse(t, recorder, http.StatusOK, "ok")
	})
	t.Run("request_context_and_two_second_limit", func(t *testing.T) {
		type contextKey struct{}
		parent := context.WithValue(context.Background(), contextKey{}, "request-value")
		handler := NewRouter(readinessCheckFunc(func(ctx context.Context) error {
			if ctx.Value(contextKey{}) != "request-value" {
				t.Fatal("request context value was lost")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 2*time.Second {
				t.Fatal("readiness check must have a two-second deadline")
			}
			return nil
		}), nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(parent))
		assertPlainResponse(t, recorder, http.StatusOK, "ok")
	})
	t.Run("request_cancellation", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		handler := NewRouter(readinessCheckFunc(func(ctx context.Context) error {
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("request cancellation was not propagated")
			}
			return ctx.Err()
		}), nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(parent))
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
	})
	t.Run("timeout", func(t *testing.T) {
		handler := NewRouter(readinessCheckFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}), nil)
		recorder := httptest.NewRecorder()
		started := time.Now()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		elapsed := time.Since(started)
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
		if elapsed < 1900*time.Millisecond || elapsed > 4*time.Second {
			t.Fatalf("unexpected readiness timeout duration: %v", elapsed)
		}
	})
	t.Run("late_success_is_not_ready", func(t *testing.T) {
		parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		handler := NewRouter(readinessCheckFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}), nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(parent))
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
	})
	t.Run("probe_each_request_and_recover", func(t *testing.T) {
		calls := 0
		handler := NewRouter(readinessCheckFunc(func(context.Context) error {
			calls++
			if calls == 1 {
				return errors.New("database unavailable")
			}
			return nil
		}), nil)
		for _, want := range []int{http.StatusServiceUnavailable, http.StatusOK} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if recorder.Code != want {
				t.Fatalf("want status %d, got %d", want, recorder.Code)
			}
		}
		if calls != 2 {
			t.Fatalf("want a new probe on each request, got %d", calls)
		}
	})
}

func assertPlainResponse(t *testing.T, recorder *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if recorder.Code != status || recorder.Body.String() != body {
		t.Fatalf("want %d %q, got %d %q", status, body, recorder.Code, recorder.Body.String())
	}
	media, _, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
	if err != nil || media != "text/plain" {
		t.Fatalf("want text/plain, got %q, %v", recorder.Header().Get("Content-Type"), err)
	}
}

func TestUserHTTPPostgres(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	store := iam.NewPostgresStore(pool)
	service := iam.NewService(store, []byte("http-fixture-key-with-at-least-32-bytes"))
	ctx := context.Background()
	if err := service.EnsureSeedAdmin(ctx, "admin@example.test", "admin-password123"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(pool, service))
	defer server.Close()
	request := func(method, path, token, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d", method, path, response.StatusCode, want)
		}
		if bytes.Contains(data, []byte("password_hash")) {
			t.Fatal("password hash leaked")
		}
		return data
	}
	login := func(email, password string) iam.LoginResponse {
		t.Helper()
		body, _ := json.Marshal(iam.LoginRequest{Email: email, Password: password})
		var result iam.LoginResponse
		if err := json.Unmarshal(request("POST", "/api/v1/auth/login", "", string(body), 200), &result); err != nil || result.Token == "" {
			t.Fatalf("login response failed: %v", err)
		}
		return result
	}

	admin := login("admin@example.test", "admin-password123")
	request("GET", "/api/v1/users", "", "", 401)
	creation := request("POST", "/api/v1/auth/register", admin.Token,
		"{\"email\":\"member@example.test\",\"password\":\"member-password123\",\"name\":\"Member\"}", 201)
	var member domain.User
	if err := json.Unmarshal(creation, &member); err != nil || member.Role != iam.GlobalRoleMember {
		t.Fatal("created account is not a member")
	}
	request("POST", "/api/v1/auth/register", admin.Token,
		"{\"email\":\"member@example.test\",\"password\":\"member-password123\",\"name\":\"Member\"}", 409)
	var users []domain.User
	if err := json.Unmarshal(request("GET", "/api/v1/users?limit=1&offset=0", admin.Token, "", 200), &users); err != nil || len(users) != 1 {
		t.Fatal("list pagination failed")
	}
	memberLogin := login(member.Email, "member-password123")
	request("GET", "/api/v1/users", memberLogin.Token, "", 403)
	request("POST", "/api/v1/auth/register", memberLogin.Token, "{}", 403)
	request("POST", "/api/v1/auth/login", "", "{\"email\":\"member@example.test\",\"password\":\"wrong\"}", 401)
	request("GET", "/api/v1/users?limit=201", admin.Token, "", 400)
	if err := store.SetUserRole(ctx, admin.User.ID, iam.GlobalRoleMember); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/v1/users", admin.Token, "", 403)
	if _, err := pool.Exec(ctx, "UPDATE users SET status='inactive' WHERE id=$1", member.ID); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/v1/users", memberLogin.Token, "", 401)
	request("GET", "/healthz", "", "", 200)
}

func newUserHTTPTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for real HTTP/PostgreSQL verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("connect to explicit test PostgreSQL")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../migrations/000001_init_iam.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid test PostgreSQL URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create HTTP fixture pool")
	}
	t.Cleanup(pool.Close)
	return pool
}
