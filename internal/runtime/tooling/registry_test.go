package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
)

func orderSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"definitions": map[string]any{"orderID": map[string]any{"type": "string", "minLength": 1}},
		"properties":  map[string]any{"order_id": map[string]any{"$ref": "#/definitions/orderID"}},
		"required":    []string{"order_id"},
	}
}

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(tool.NewEndpointPolicy(nil, false))
	if err != nil {
		t.Fatal(err)
	}
	var called int
	registry.RegisterSDK("orders", ExecutorFunc(func(_ context.Context, args json.RawMessage) (string, error) {
		called++
		return string(args), nil
	}))
	target := &tool.ExecutionConfig{
		Tool: &domain.Tool{ID: "get_order"}, VersionID: "version-2",
		Config: &tool.ToolConfig{
			SourceType: "internal_sdk", Schema: orderSchema(),
			EndpointConfig: map[string]any{"sdk_name": "orders"},
		},
	}
	built, err := registry.Build(target)
	if err != nil || built.ToolID != "get_order" || built.VersionID != "version-2" {
		t.Fatalf("build: %v", err)
	}
	for _, args := range []string{`{}`, `{"order_id":2}`, `{"order_id":""}`, `{"order_id":`} {
		if _, err := built.Executor.Execute(ctx, json.RawMessage(args)); err == nil {
			t.Fatalf("invalid arguments accepted: %s", args)
		}
	}
	if called != 0 {
		t.Fatalf("invalid arguments reached the executor %d times", called)
	}
	out, err := built.Executor.Execute(ctx, json.RawMessage(`{"order_id":"fixture"}`))
	if err != nil || out != `{"order_id":"fixture"}` || called != 1 {
		t.Fatalf("valid arguments: %s, %v, calls=%d", out, err, called)
	}

	target.Tool.Sensitive = true
	if _, err := registry.Build(target); !errors.Is(err, ErrSensitiveToolRequiresApproval) {
		t.Fatalf("sensitive tool was not blocked: %v", err)
	}
	target.Tool.Sensitive = false
	target.Config.EndpointConfig["sdk_name"] = "missing"
	if _, err := registry.Build(target); err == nil {
		t.Fatal("unregistered SDK accepted")
	}
}

func TestToolSchemaAllowsOnlyInlineReferences(t *testing.T) {
	var requested atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requested.Store(true)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()
	schema := map[string]any{"$ref": server.URL + "/schema.json"}
	exec := ExecutorFunc(func(context.Context, json.RawMessage) (string, error) {
		t.Fatal("executor should not run")
		return "", nil
	})
	if _, err := withSchemaValidation(exec, schema); err == nil || !strings.Contains(err.Error(), "external tool schema references are disabled") {
		t.Fatalf("external reference did not fail compilation: %v", err)
	}
	if requested.Load() {
		t.Fatal("compile fetched an external schema")
	}
}

func TestRegistryCrossborderFixture(t *testing.T) {
	ctx := context.Background()
	url := os.Getenv("ASTERFLOW_TEST_CROSSBORDER_TOOL_URL")
	if url == "" {
		t.Skip("isolated Crossborder fixture is not running")
	}
	policy := tool.NewEndpointPolicy([]string{"127.0.0.1"}, false)
	if err := policy.ValidateURL(ctx, url); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.client.CloseIdleConnections()
	built, err := registry.Build(&tool.ExecutionConfig{
		Tool: &domain.Tool{ID: "get_order"}, VersionID: "version-2",
		Config: &tool.ToolConfig{
			SourceType: "rest_api", Schema: orderSchema(), EndpointConfig: map[string]any{"url": url},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := built.Executor.Execute(ctx, json.RawMessage(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "violates schema") {
		t.Fatalf("expected schema rejection: %v", err)
	}
	out, err := built.Executor.Execute(ctx, json.RawMessage(`{"order_id":"TTS-20260801-1001"}`))
	if err != nil || !strings.Contains(out, "TTS-20260801-1001") {
		t.Fatalf("registry-built REST executor failed: %v", err)
	}
}

func TestRegistryEinoToolNode(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(tool.NewEndpointPolicy(nil, false))
	if err != nil {
		t.Fatal(err)
	}
	registry.RegisterSDK("orders", ExecutorFunc(func(_ context.Context, args json.RawMessage) (string, error) {
		return string(args), nil
	}))
	target := &tool.ExecutionConfig{
		Tool: &domain.Tool{ID: "tool-orders", Name: "get_order", Description: "查询订单"},
		VersionID: "version-one",
		Config: &tool.ToolConfig{
			SourceType: "internal_sdk", Schema: orderSchema(),
			EndpointConfig: map[string]any{"sdk_name": "orders"},
			AuthConfig: map[string]any{"bearer_token": "private-fixture-value"},
		},
	}
	adapter, err := registry.BuildInvokable(target)
	if err != nil {
		t.Fatal(err)
	}
	info, err := adapter.Info(ctx)
	if err != nil || info.Name != "get_order" || info.Desc != "查询订单" || info.ParamsOneOf == nil {
		t.Fatalf("unexpected Eino tool info: %#v, %v", info, err)
	}
	spec, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	specJSON, err := json.Marshal(spec)
	if err != nil || !strings.Contains(string(specJSON), "#/definitions/orderID") ||
		!strings.Contains(string(specJSON), "\"definitions\"") ||
		!strings.Contains(string(specJSON), "\"required\":[\"order_id\"]") ||
		strings.Contains(string(specJSON), "private-fixture-value") {
		t.Fatalf("unexpected model-facing schema: %s, %v", specJSON, err)
	}

	node, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{Tools: []einotool.BaseTool{adapter}})
	if err != nil {
		t.Fatal(err)
	}
	call := schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call_order_one", Type: "function",
		Function: schema.FunctionCall{Name: "get_order", Arguments: "{}"},
	}})
	if _, err := node.Invoke(ctx, call); err == nil || !strings.Contains(err.Error(), "order_id is required") {
		t.Fatalf("Eino should surface the existing schema validation: %v", err)
	}
	call.ToolCalls[0].Function.Arguments = "{\"order_id\":\"TTS-20260801-1001\"}"
	result, err := node.Invoke(ctx, call)
	if err != nil || len(result) != 1 || result[0].Role != schema.Tool ||
		result[0].ToolCallID != "call_order_one" || result[0].Content != call.ToolCalls[0].Function.Arguments {
		t.Fatalf("unexpected Eino tool result: %#v, %v", result, err)
	}

	target.Tool.Sensitive = true
	if _, err := registry.BuildInvokable(target); !errors.Is(err, ErrSensitiveToolRequiresApproval) {
		t.Fatalf("sensitive tool was exposed to Eino: %v", err)
	}
}
