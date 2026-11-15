package api

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type readinessCheckFunc func(context.Context) error

func (f readinessCheckFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestNewRouter(t *testing.T) {
	handler := NewRouter(readinessCheckFunc(func(context.Context) error { return nil }))
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
		NewRouter(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
	})
	t.Run("dependency_error_is_not_exposed", func(t *testing.T) {
		calls := 0
		handler := NewRouter(readinessCheckFunc(func(context.Context) error {
			calls++
			return errors.New("private database error must not be exposed")
		}))
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
		}))
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
		}))
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
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(parent))
		assertPlainResponse(t, recorder, http.StatusServiceUnavailable, "not ready")
	})
	t.Run("timeout", func(t *testing.T) {
		handler := NewRouter(readinessCheckFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}))
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
		}))
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
		}))
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
