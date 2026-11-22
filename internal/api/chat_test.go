package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
	"github.com/yz1qaq/asterflow/internal/platform/conversation"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

// fakeChatRunner stands in for the real engine so no model is called.
type fakeChatRunner struct{}

func (fakeChatRunner) Run(ctx context.Context, in engine.RunInput, emit engine.Emitter) (*engine.RunResult, error) {
	if err := emit.Emit(ctx, engine.Event{Seq: 1, Type: engine.EventRunStarted,
		Data: engine.EncodeData(engine.RunStarted{ConversationID: in.ConversationID})}); err != nil {
		return nil, err
	}
	if err := emit.Emit(ctx, engine.Event{Seq: 2, Type: engine.EventModelDelta,
		Data: engine.EncodeData(engine.ModelDelta{Content: "hi"})}); err != nil {
		return nil, err
	}
	return &engine.RunResult{Content: "hi", Model: "fixture", Usage: llm.Usage{TotalTokens: 2}, NextSeq: 2}, nil
}

func TestConversationChatSSE(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	ctx := context.Background()
	for _, migration := range []string{"000009_agents.up.sql", "000010_conversations.up.sql"} {
		script, err := os.ReadFile("../../migrations/" + migration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(script)); err != nil {
			t.Fatal(err)
		}
	}

	identities := iam.NewService(iam.NewPostgresStore(pool), []byte("chat-sse-fixture-key-at-least-32-bytes"))
	owner, err := identities.CreateUser(ctx, "owner@chat.test", "fixture-password123", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	space, err := identities.CreateWorkspaceForUser(ctx, owner.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	login, err := identities.Login(ctx, iam.LoginRequest{Email: "owner@chat.test", Password: "fixture-password123"})
	if err != nil {
		t.Fatal(err)
	}
	token := login.Token

	models := modelconfig.NewService(modelconfig.NewPostgresStore(pool), nil)
	prompts := prompt.NewService(prompt.NewPostgresStore(pool), models)
	agents := agent.NewService(agent.NewPostgresStore(pool), prompts, models)
	conversations := conversation.NewService(conversation.NewPostgresStore(pool), agents, nil, fakeChatRunner{})
	server := httptest.NewServer(NewRouter(pool, identities, models, prompts,
		Services{Agent: agents, Conversation: conversations}))
	defer server.Close()

	do := func(method, path, body, tok string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Workspace-ID", space.ID)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res, raw
	}

	marshal := func(v any) string {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	res, raw := do("POST", "/api/v1/agents", marshal(map[string]any{
		"name": "Support", "system_prompt": "be concise",
	}), token)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create agent = %d %s", res.StatusCode, raw)
	}
	var created struct {
		Agent   domain.Agent        `json:"agent"`
		Version domain.AgentVersion `json:"version"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}

	res, raw = do("POST", "/api/v1/conversations", marshal(map[string]any{
		"agent_id": created.Agent.ID, "agent_version_id": created.Version.ID,
	}), token)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create conversation = %d %s", res.StatusCode, raw)
	}
	var conv domain.Conversation
	if err := json.Unmarshal(raw, &conv); err != nil {
		t.Fatal(err)
	}

	// Empty input must be rejected before streaming.
	res, raw = do("POST", "/api/v1/conversations/"+conv.ID+"/chat", marshal(map[string]any{"input": " "}), token)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank input = %d %s", res.StatusCode, raw)
	}

	res, raw = do("POST", "/api/v1/conversations/"+conv.ID+"/chat", marshal(map[string]any{"input": "hi"}), token)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("chat = %d %s", res.StatusCode, raw)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}

	var events []engine.Event
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event engine.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		events = append(events, event)
	}
	wantTypes := []engine.EventType{engine.EventRunStarted, engine.EventModelDelta, engine.EventRunCompleted}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %+v", events)
	}
	for i, want := range wantTypes {
		if events[i].Type != want || events[i].Seq != int64(i+1) {
			t.Fatalf("event %d = %+v, want type %s", i, events[i], want)
		}
	}
	if events[2].Data == nil || !strings.Contains(string(events[2].Data), "assistant_message_id") {
		t.Fatalf("completed event data = %s", events[2].Data)
	}

	messages, err := conversations.Messages(ctx, space.ID, conv.ID, owner.ID, false)
	if err != nil || len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" || messages[1].Content != "hi" {
		t.Fatalf("messages = %+v, %v", messages, err)
	}

	// Other user must not chat in this conversation.
	other, err := identities.CreateUser(ctx, "other@chat.test", "fixture-password123", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identities.UpsertWorkspaceMember(ctx, owner.ID, space.ID, other.ID, iam.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	otherLogin, err := identities.Login(ctx, iam.LoginRequest{Email: "other@chat.test", Password: "fixture-password123"})
	if err != nil {
		t.Fatal(err)
	}
	res, raw = do("POST", "/api/v1/conversations/"+conv.ID+"/chat", marshal(map[string]any{"input": "hi"}), otherLogin.Token)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("other user chat = %d %s", res.StatusCode, raw)
	}
}

func TestConversationChatInvalidID(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	ctx := context.Background()
	for _, migration := range []string{"000009_agents.up.sql", "000010_conversations.up.sql"} {
		script, err := os.ReadFile("../../migrations/" + migration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(script)); err != nil {
			t.Fatal(err)
		}
	}
	identities := iam.NewService(iam.NewPostgresStore(pool), []byte("chat-invalid-fixture-key-32-bytes"))
	owner, err := identities.CreateUser(ctx, "owner2@chat.test", "fixture-password123", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	space, err := identities.CreateWorkspaceForUser(ctx, owner.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	login, err := identities.Login(ctx, iam.LoginRequest{Email: "owner2@chat.test", Password: "fixture-password123"})
	if err != nil {
		t.Fatal(err)
	}
	models := modelconfig.NewService(modelconfig.NewPostgresStore(pool), nil)
	prompts := prompt.NewService(prompt.NewPostgresStore(pool), models)
	agents := agent.NewService(agent.NewPostgresStore(pool), prompts, models)
	conversations := conversation.NewService(conversation.NewPostgresStore(pool), agents, nil, fakeChatRunner{})
	server := httptest.NewServer(NewRouter(pool, identities, models, prompts,
		Services{Agent: agents, Conversation: conversations}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/conversations/not-a-uuid/chat", strings.NewReader(`{"input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+login.Token)
	req.Header.Set("X-Workspace-ID", space.ID)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("invalid id = %d %s", res.StatusCode, raw)
	}
	_ = uuid.NewString
}
