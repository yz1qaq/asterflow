package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

type streamingGateway struct {
	llm.Gateway
	stream llm.Stream
}

func (g streamingGateway) Stream(context.Context, string, string, llm.GenerateRequest) (llm.Stream, error) {
	return g.stream, nil
}

type cancellableModelStream struct {
	started bool
	closed  chan struct{}
}

func (s *cancellableModelStream) Recv(ctx context.Context) (*llm.StreamChunk, error) {
	if !s.started {
		s.started = true
		return &llm.StreamChunk{Content: "partial"}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *cancellableModelStream) Close() error {
	close(s.closed)
	return nil
}

type captureEvent func(context.Context, Event) error

func (f captureEvent) Emit(ctx context.Context, ev Event) error {
	return f(ctx, ev)
}

func TestADKTurnCancellationClosesGatewayStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream := &cancellableModelStream{closed: make(chan struct{})}
	executor := NewExecutor(nil, streamingGateway{stream: stream})
	sawDelta := false
	_, err := executor.Run(ctx, RunInput{
		ConversationID: "conversation", AgentVersionID: "v1", TurnRevision: 4,
		RuntimeConfigJSON: `{}`, UserInput: "hello",
	}, captureEvent(func(_ context.Context, event Event) error {
		if event.Type == EventModelDelta {
			sawDelta = true
			cancel()
		}
		return nil
	}))
	if !sawDelta || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run: saw delta = %t, err = %v", sawDelta, err)
	}
	select {
	case <-stream.closed:
	case <-time.After(time.Second):
		t.Fatal("ADK did not release the Gateway stream")
	}
}

type fixtureToolBuilder func(context.Context, string, string) (einotool.InvokableTool, domain.ToolIdentity, error)

func (f fixtureToolBuilder) BuildByVersion(ctx context.Context, workspaceID, versionID string) (einotool.InvokableTool, domain.ToolIdentity, error) {
	return f(ctx, workspaceID, versionID)
}

type fixtureInvokable struct{ name, value string }

func (t fixtureInvokable) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t fixtureInvokable) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return t.value, nil
}

func TestBuildToolsFromSnapshot(t *testing.T) {
	ctx := context.Background()
	executor := NewExecutor(nil, nil)
	if tools, _, err := executor.buildTools(ctx, RunInput{}); err != nil || len(tools) != 0 {
		t.Fatalf("empty tool list: %v, %v", tools, err)
	}
	in := RunInput{WorkspaceID: "orders-space", ToolVersionIDs: []string{"order-v1", "inventory-v3"}}
	if _, _, err := executor.buildTools(ctx, in); err == nil {
		t.Fatal("accepts requested tool versions without a builder")
	}
	unknown := errors.New("fixture version does not exist")
	names := map[string]string{"order-v1": "get_order", "order-v2": "get_order", "inventory-v3": "get_inventory"}
	executor.ConfigureTools(fixtureToolBuilder(func(_ context.Context, space, id string) (einotool.InvokableTool, domain.ToolIdentity, error) {
		if space != in.WorkspaceID {
			t.Fatalf("wrong tool lookup workspace: %s", space)
		}
		name := names[id]
		if name == "" {
			return nil, domain.ToolIdentity{}, unknown
		}
		return fixtureInvokable{name: name, value: id}, domain.ToolIdentity{ToolID: name + "-tool", VersionID: id}, nil
	}))
	tools, identities, err := executor.buildTools(ctx, in)
	if err != nil || len(tools) != 2 {
		t.Fatalf("build tools: %v, %v", tools, err)
	}
	wantOrder := domain.ToolIdentity{ToolID: "get_order-tool", VersionID: "order-v1"}
	wantInventory := domain.ToolIdentity{ToolID: "get_inventory-tool", VersionID: "inventory-v3"}
	if identities["get_order"] != wantOrder || identities["get_inventory"] != wantInventory {
		t.Fatalf("wrong tool identities: %v", identities)
	}
	node, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{Tools: tools, ExecuteSequentially: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := node.Invoke(ctx, schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-1", Type: "function",
		Function: schema.FunctionCall{Name: "get_order", Arguments: "{}"},
	}}))
	if err != nil || len(result) != 1 || result[0].Content != "order-v1" {
		t.Fatalf("wrong snapshot tool executed: %+v (%v)", result, err)
	}
	in.ToolVersionIDs = []string{"order-v1", "order-v2"}
	if _, _, err := executor.buildTools(ctx, in); err == nil || !strings.Contains(err.Error(), "duplicate tool name") {
		t.Fatalf("ambiguous model-facing tool names were accepted: %v", err)
	}
	in.ToolVersionIDs = []string{"missing"}
	if _, _, err := executor.buildTools(ctx, in); !errors.Is(err, unknown) {
		t.Fatalf("original build error was not preserved: %v", err)
	}
}

type scriptedToolGateway struct {
	llm.Gateway
	requests []llm.GenerateRequest
}

func (g *scriptedToolGateway) Stream(ctx context.Context, space, classification string, req llm.GenerateRequest) (llm.Stream, error) {
	g.requests = append(g.requests, req)
	index := 0
	if len(g.requests) == 1 {
		return &toolFixtureStream{chunks: []llm.StreamChunk{
			{Content: "pre-tool narration"},
			{ToolCalls: []llm.ToolCall{{
				Index: &index, ID: "call-order", Type: "function",
				Function: schema.FunctionCall{Name: "get_order", Arguments: "{"},
			}}},
			{ToolCalls: []llm.ToolCall{{Index: &index, Function: schema.FunctionCall{Arguments: "}"}}}},
			{Done: true, Model: "first", Usage: llm.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}},
		}}, nil
	}
	if len(g.requests) != 2 {
		return nil, errors.New("unexpected number of model calls")
	}
	return &toolFixtureStream{chunks: []llm.StreamChunk{
		{Content: "等待"},
		{Content: "发货"},
		{Done: true, Model: "last", Usage: llm.Usage{PromptTokens: 6, CompletionTokens: 3, TotalTokens: 9}},
	}}, nil
}

type toolFixtureStream struct {
	chunks []llm.StreamChunk
}

func (s *toolFixtureStream) Recv(ctx context.Context) (*llm.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &chunk, nil
}

func (s *toolFixtureStream) Close() error { return nil }

func TestADKInvokesPinnedToolAndFeedsResultBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gateway := &scriptedToolGateway{}
	executor := NewExecutor(nil, gateway)
	executor.ConfigureTools(fixtureToolBuilder(func(_ context.Context, space, version string) (einotool.InvokableTool, domain.ToolIdentity, error) {
		if space != "tenant-one" || version != "order-version-one" {
			t.Fatalf("tool resolution: %s, %s", space, version)
		}
		return fixtureInvokable{name: "get_order", value: "awaiting_shipment"}, domain.ToolIdentity{ToolID: "order-tool", VersionID: version}, nil
	}))
	var emitted []string
	var events []Event
	result, err := executor.Run(ctx, RunInput{
		WorkspaceID: "tenant-one", RuntimeConfigJSON: "{\"system_prompt\":\"customer support\"}",
		ToolVersionIDs: []string{"order-version-one"}, UserInput: "When will it ship?",
	}, captureEvent(func(_ context.Context, event Event) error {
		events = append(events, event)
		if event.Type == EventModelDelta {
			var delta ModelDelta
			if err := json.Unmarshal(event.Data, &delta); err != nil {
				return err
			}
			emitted = append(emitted, delta.Content)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "等待发货" || strings.Join(emitted, "") != result.Content || result.Model != "last" ||
		result.Usage != (llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}) {
		t.Fatalf("result = %+v, emitted = %v", result, emitted)
	}
	if len(gateway.requests) != 2 {
		t.Fatalf("got %d calls to the model", len(gateway.requests))
	}
	if len(events) != 5 || events[2].Type != EventToolCall || events[3].Type != EventToolResult ||
		events[2].Seq+1 != events[3].Seq || events[4].Type != EventModelDelta {
		t.Fatalf("unexpected tool event order: %+v", events)
	}
	var started ToolCallEvent
	var finished ToolResultEvent
	if err := json.Unmarshal(events[2].Data, &started); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(events[3].Data, &finished); err != nil {
		t.Fatal(err)
	}
	if started.CallID != "call-order" || started.CallID != finished.CallID ||
		started.Arguments != "{}" || finished.Status != "success" || finished.Output != "awaiting_shipment" {
		t.Fatalf("tool event payload: %+v %+v", started, finished)
	}
	first, second := gateway.requests[0], gateway.requests[1]
	if len(first.Tools) != 1 || first.Tools[0].Name != "get_order" || first.SystemPrompt != "customer support" {
		t.Fatalf("model did not receive the pinned tool: %+v", first)
	}
	if len(second.Messages) != 3 {
		t.Fatalf("unexpected second request messages: %+v", second.Messages)
	}
	call, tool := second.Messages[1], second.Messages[2]
	if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].Function.Arguments != "{}" ||
		tool.Role != "tool" || tool.Content != "awaiting_shipment" || tool.ToolCallID != call.ToolCalls[0].ID {
		t.Fatalf("tool output was not paired with the model call: %+v %+v", call, tool)
	}
}

func TestToolEventMiddlewareReportsRecoverableFailure(t *testing.T) {
	var types []EventType
	var reported ToolResultEvent
	middleware := toolEventMiddleware(func(kind EventType, data any) error {
		types = append(types, kind)
		if kind == EventToolResult {
			reported = data.(ToolResultEvent)
		}
		return nil
	})
	result, err := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		if len(types) != 1 || types[0] != EventToolCall {
			t.Fatalf("call event missing before execution: %v", types)
		}
		return nil, errors.New("fixture error with internal metadata")
	})(context.Background(), &compose.ToolInput{Name: "get_order", CallID: "failed-call", Arguments: "{}"})
	if err != nil || result.Result != "error: tool execution failed" {
		t.Fatalf("tool failure should return a model-visible result: %v %v", result, err)
	}
	if len(types) != 2 || types[1] != EventToolResult || reported.CallID != "failed-call" ||
		reported.Status != "error" || reported.Error != "工具执行失败" || reported.Output != "" {
		t.Fatalf("tool failure event: %v %+v", types, reported)
	}
}

func TestToolHistoryRetainsCallAndResultAtWindowBoundary(t *testing.T) {
	callID := "provider_1"
	history := []*domain.Message{
		{Role: "user", Content: "查订单"},
		{Role: "assistant", ToolCalls: []domain.ToolCall{{ID: "database-id", ProviderCallID: callID, Name: "get_order", Args: "not-json"}}},
		{Role: "tool", ToolCallID: &callID, Content: "工具失败"},
		{Role: "assistant", Content: "请提供订单号"},
	}
	for range 4 {
		history = append(history, &domain.Message{Role: "user", Content: "你好"}, &domain.Message{Role: "assistant", Content: "你好"})
	}
	got, err := buildModelMessages(history, "继续")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 13 || len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != callID ||
		got[1].ToolCalls[0].Function.Arguments != "{}" || got[2].ToolCallID != callID {
		t.Fatalf("tool pair broken at boundary: %+v", got)
	}
}

func TestADKReportsUnknownToolAndFeedsFailureBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gateway := &scriptedToolGateway{}
	executor := NewExecutor(nil, gateway)
	executor.ConfigureTools(fixtureToolBuilder(func(_ context.Context, _, version string) (einotool.InvokableTool, domain.ToolIdentity, error) {
		return fixtureInvokable{name: "get_inventory", value: "unused"}, domain.ToolIdentity{ToolID: "inventory-tool", VersionID: version}, nil
	}))

	var events []Event
	result, err := executor.Run(ctx, RunInput{
		WorkspaceID: "tenant-one", RuntimeConfigJSON: "{}",
		ToolVersionIDs: []string{"inventory-v1"}, UserInput: "When will it ship?",
	}, captureEvent(func(_ context.Context, event Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[2].Type != EventToolCall || events[3].Type != EventToolResult ||
		events[2].Seq+1 != events[3].Seq || events[4].Type != EventModelDelta {
		t.Fatalf("unknown tool did not emit an ordered call/result pair: %+v", events)
	}
	var launched ToolCallEvent
	if err := json.Unmarshal(events[2].Data, &launched); err != nil {
		t.Fatal(err)
	}
	var failed ToolResultEvent
	if err := json.Unmarshal(events[3].Data, &failed); err != nil {
		t.Fatal(err)
	}
	if launched.CallID != "call-order" || launched.Name != "get_order" || launched.Arguments != "{}" ||
		failed.CallID != launched.CallID || failed.Name != launched.Name || failed.Status != "error" || failed.Error == "" {
		t.Fatalf("unexpected failure events: %+v %+v", launched, failed)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0] != failed {
		t.Fatalf("tool result for storage differs from the SSE failure: %+v", result.ToolResults)
	}
	if len(gateway.requests) != 2 || len(result.ToolMessages) != 2 {
		t.Fatalf("missing model retry or tool transcript: %d %+v", len(gateway.requests), result.ToolMessages)
	}
	tool := gateway.requests[1].Messages[len(gateway.requests[1].Messages)-1]
	if tool.Role != "tool" || tool.ToolCallID != launched.CallID ||
		tool.Content != `error: tool "get_order" is not available` ||
		result.ToolMessages[1].ToolCallID != launched.CallID || result.ToolMessages[1].Content != tool.Content {
		t.Fatalf("model and stored transcript must receive fallback result: %+v %+v", tool, result.ToolMessages)
	}
}
