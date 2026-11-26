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

	"github.com/cloudwego/eino/schema"
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

func TestEinoOpenAIChatTools(t *testing.T) {
	ctx := context.Background()
	type observed struct {
		path string
		req  map[string]json.RawMessage
		auth string
		err  error
	}
	requests := make(chan observed, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]json.RawMessage
		err := json.NewDecoder(r.Body).Decode(&fields)
		requests <- observed{r.URL.Path, fields, r.Header.Get("Authorization"), err}
		if string(fields["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, data := range []string{
				"{\"model\":\"provider-resolved\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_order\",\"arguments\":\"{\\\"order\"}}]}}]}",
				"{\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"_id\\\":\\\"A\\\"}\"}}]}}]}",
				"{\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}",
				"{\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}",
				"[DONE]",
			} {
				_, _ = io.WriteString(w, "data: "+data+"\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"model\":\"provider-resolved\",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_order\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"total_tokens\":8}}")
	}))
	defer server.Close()

	client := NewEinoOpenAIClient()
	dep := ResolvedDeployment{ID: "deploy", BaseURL: server.URL + "/v1", Model: "model-a", APIKey: "fixture"}
	req := GenerateRequest{Tools: []*schema.ToolInfo{{Name: "get_order", Desc: "find an order", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"order_id": {Type: schema.String, Required: true},
	})}}, Messages: []Message{{Role: "user", Content: "find A"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_old", Type: "function", Function: schema.FunctionCall{Name: "get_order", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "call_old", Content: "in transit"},
	}}
	one, err := client.Generate(ctx, dep, req)
	if err != nil || len(one.ToolCalls) != 1 || one.ToolCalls[0].ID != "call_1" || one.Usage.TotalTokens != 8 || one.Model != "provider-resolved" {
		t.Fatalf("generate result: %+v (%v)", one, err)
	}
	first := <-requests
	if first.err != nil || first.path != "/v1/chat/completions" || first.auth != "Bearer fixture" ||
		!strings.Contains(string(first.req["tools"]), "\"name\":\"get_order\"") ||
		!strings.Contains(string(first.req["messages"]), "\"tool_call_id\":\"call_old\"") {
		t.Fatalf("unexpected wire request: %+v", first)
	}
	stream, err := client.Stream(ctx, dep, req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var merged []*schema.Message
	for {
		delta, err := stream.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if delta.Done {
			if delta.Usage.TotalTokens != 6 || delta.Model != "provider-resolved" || delta.DeploymentID != "deploy" {
				t.Fatalf("done: %+v", delta)
			}
			break
		}
		merged = append(merged, schema.AssistantMessage(delta.Content, delta.ToolCalls))
	}
	msg, err := schema.ConcatMessages(merged)
	if err != nil || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Arguments != "{\"order_id\":\"A\"}" {
		t.Fatalf("joined tool args: %+v (%v)", msg, err)
	}
}

func TestEinoOpenAIRetryableStatus(t *testing.T) {
	for _, tc := range []struct {
		status    int
		retryable bool
	}{{429, true}, {502, true}, {401, false}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, "provider error fixture")
		}))
		_, err := NewEinoOpenAIClient().Generate(context.Background(), ResolvedDeployment{BaseURL: s.URL, Model: "m"}, GenerateRequest{})
		s.Close()
		var failure *ProviderError
		if !errors.As(err, &failure) || failure.StatusCode != tc.status || failure.Retryable != tc.retryable {
			t.Fatalf("status %d: %v", tc.status, err)
		}
	}
}

func TestEinoOpenAIStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"))
		flusher.Flush()
		<-r.Context().Done() // 阻塞直到客户端断开
	}))
	defer server.Close()

	client := NewEinoOpenAIClient()
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

func TestEinoOpenAIStreamRejectsPrematureClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
	}))
	defer server.Close()
	ctx := context.Background()
	stream, err := NewEinoOpenAIClient().Stream(ctx, ResolvedDeployment{BaseURL: server.URL, Model: "fixture"}, GenerateRequest{})
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
