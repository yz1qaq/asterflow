package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yz1qaq/asterflow/internal/platform/tool"
)

func TestRESTExecutor(t *testing.T) {
	ctx := context.Background()
	fixtureInput := json.RawMessage(`{"order_id":"fixture-1"}`)
	var redirected atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orders":
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != string(fixtureInput) || r.Method != "POST" ||
				r.Header.Get("Authorization") != "Bearer fixture-credential" ||
				r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected order request: method=%s, read error=%v", r.Method, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"order_id":"fixture-1"}`)
		case "/redirect-same":
			http.Redirect(w, r, "/orders", http.StatusTemporaryRedirect)
		case "/redirect-other":
			http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
		case "/long":
			_, _ = io.WriteString(w, strings.Repeat("a", maxToolResponseBytes+1))
		case "/failure":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "fixture-private-upstream-text")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer source.Close()
	policy := tool.NewEndpointPolicy([]string{"127.0.0.1"}, false)
	client, err := policy.HTTPClient(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	makeExecutor := func(client *http.Client, path string) Executor {
		return NewRESTExecutor(client, &tool.ToolConfig{
			EndpointConfig: map[string]any{"url": source.URL + path},
			AuthConfig:     map[string]any{"header": "Authorization", "value": "Bearer fixture-credential"},
		})
	}
	for _, path := range []string{"/orders", "/redirect-same"} {
		out, err := makeExecutor(client, path).Execute(ctx, fixtureInput)
		if err != nil || out != string(fixtureInput) {
			t.Fatalf("%s: output = %q, error = %v", path, out, err)
		}
	}
	for _, tc := range []struct{ path, message string }{
		{"/redirect-other", "cross-origin redirect is blocked"},
		{"/long", "tool response exceeds"},
		{"/failure", "rest tool returned HTTP 502"},
	} {
		out, err := makeExecutor(client, tc.path).Execute(ctx, fixtureInput)
		if out != "" || err == nil || !strings.Contains(err.Error(), tc.message) ||
			strings.Contains(err.Error(), "fixture-private-upstream-text") {
			t.Fatalf("%s: output = %q, error = %v", tc.path, out, err)
		}
	}
	if redirected.Load() {
		t.Fatal("redirected origin received a request")
	}
	strictClient, err := tool.NewEndpointPolicy(nil, false).HTTPClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer strictClient.CloseIdleConnections()
	if _, err := makeExecutor(strictClient, "/orders").Execute(ctx, fixtureInput); err == nil ||
		!strings.Contains(err.Error(), "private or local network") {
		t.Fatalf("loopback was not rejected: %v", err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := makeExecutor(client, "/orders?region=us").Execute(stopped, fixtureInput); !errors.Is(err, context.Canceled) ||
		strings.Contains(err.Error(), "region=") {
		t.Fatalf("expected cancellation without including endpoint query: %v", err)
	}
}

type failingRESTTransport struct{}

func (failingRESTTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("fixture-transport-detail")
}

func TestRESTExecutorPublicTransportError(t *testing.T) {
	client := &http.Client{Transport: failingRESTTransport{}}
	executor := NewRESTExecutor(client, &tool.ToolConfig{
		EndpointConfig: map[string]any{"url": "https://example.test/orders"},
	})
	output, err := executor.Execute(context.Background(), json.RawMessage(`{}`))
	if output != "" || err == nil || err.Error() != "call rest tool: upstream request failed" {
		t.Fatalf("want empty output and standardized upstream error, got output=%q, error=%v", output, err)
	}
}

func TestRESTCrossborderFixture(t *testing.T) {
	endpoint := os.Getenv("ASTERFLOW_TEST_CROSSBORDER_TOOL_URL")
	if endpoint == "" {
		t.Skip("isolated Crossborder fixture is not running")
	}
	policy := tool.NewEndpointPolicy([]string{"127.0.0.1"}, false)
	if err := policy.ValidateURL(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	client, err := policy.HTTPClient(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	executor := NewRESTExecutor(client, &tool.ToolConfig{EndpointConfig: map[string]any{"url": endpoint}})
	out, err := executor.Execute(context.Background(), json.RawMessage(`{"order_id":"TTS-20260801-1001"}`))
	if err != nil || !strings.Contains(out, "TTS-20260801-1001") || !strings.Contains(out, "SKU-BLACK-M-01") {
		t.Fatalf("business fixture output=%q, err=%v", out, err)
	}
	if _, err := executor.Execute(context.Background(), json.RawMessage(`{"order_id":"missing-order"}`)); err == nil ||
		!strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("missing order: %v", err)
	}
}
