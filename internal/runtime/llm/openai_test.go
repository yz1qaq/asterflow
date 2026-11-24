package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIGenerate(t *testing.T) {
	var got openAIRequest
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-test","choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()

	client := NewOpenAIClient()
	temperature := float32(0.2)
	res, err := client.Generate(context.Background(), ResolvedDeployment{
		ID: "d1", BaseURL: server.URL, APIKey: "secret", Model: "gpt-test",
	}, GenerateRequest{
		SystemPrompt: "sys", Messages: []Message{{Role: "user", Content: "hi"}},
		Temperature: &temperature, MaxOutputTokens: intPtr(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "hello" || res.Model != "gpt-test" || res.DeploymentID != "d1" ||
		res.Usage.TotalTokens != 3 {
		t.Fatalf("response = %+v", res)
	}
	if auth != "Bearer secret" {
		t.Fatalf("auth header = %q", auth)
	}
	if got.Model != "gpt-test" || got.Stream || got.StreamOptions != nil {
		t.Fatalf("request = %+v", got)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[0].Content != "sys" ||
		got.Messages[1].Role != "user" || got.Messages[1].Content != "hi" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if got.Temperature == nil || *got.Temperature != 0.2 || got.MaxTokens == nil || *got.MaxTokens != 100 {
		t.Fatalf("generation params = %+v", got)
	}
}

func TestOpenAIStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":5}}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAIClient()
	stream, err := client.Stream(context.Background(), ResolvedDeployment{ID: "d1", BaseURL: server.URL, Model: "gpt-test"}, GenerateRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var content strings.Builder
	for {
		chunk, err := stream.Recv(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
		}
		if chunk.Done {
			if content.String() != "hello" || chunk.Model != "gpt-test" || chunk.Usage.TotalTokens != 5 || chunk.DeploymentID != "d1" {
				t.Fatalf("stream done = %q model=%q usage=%+v dep=%q", content.String(), chunk.Model, chunk.Usage, chunk.DeploymentID)
			}
			break
		}
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIRetryableStatus(t *testing.T) {
	for _, tc := range []struct {
		status    int
		retryable bool
	}{
		{429, true}, {500, true}, {503, true},
		{400, false}, {401, false}, {403, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte("nope"))
		}))
		client := NewOpenAIClient()
		_, err := client.Generate(context.Background(), ResolvedDeployment{ID: "d1", BaseURL: server.URL, Model: "m"}, GenerateRequest{})
		server.Close()
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("status %d: expected ProviderError, got %v", tc.status, err)
		}
		if providerErr.Retryable != tc.retryable {
			t.Fatalf("status %d: retryable = %v, want %v", tc.status, providerErr.Retryable, tc.retryable)
		}
	}
}

func TestOpenAIStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
		flusher.Flush()
		<-r.Context().Done() // 阻塞直到客户端断开
	}))
	defer server.Close()

	client := NewOpenAIClient()
	stream, err := client.Stream(ctx, ResolvedDeployment{ID: "d1", BaseURL: server.URL, Model: "m"}, GenerateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(ctx); err != nil {
		t.Fatalf("first chunk should arrive before cancel: %v", err)
	}
	cancel()
	if _, err := stream.Recv(ctx); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func intPtr(value int) *int { return &value }

func TestOpenAIStreamRejectsPrematureClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
	}))
	defer server.Close()
	ctx := context.Background()
	stream, err := NewOpenAIClient().Stream(ctx, ResolvedDeployment{BaseURL: server.URL, Model: "fixture"}, GenerateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if chunk, err := stream.Recv(ctx); err != nil || chunk.Content != "partial" {
		t.Fatalf("first chunk = %+v, %v", chunk, err)
	}
	if chunk, err := stream.Recv(ctx); chunk != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("premature close = %+v, %v", chunk, err)
	}
}
