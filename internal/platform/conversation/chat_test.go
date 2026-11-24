package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

type captureEmitter struct {
	mu     sync.Mutex
	events []engine.Event
}

func (e *captureEmitter) Emit(_ context.Context, event engine.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	return nil
}

type fakeRunner struct {
	result *engine.RunResult
	err    error
}

func (f fakeRunner) Run(_ context.Context, _ engine.RunInput, emit engine.Emitter) (*engine.RunResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if err := emit.Emit(context.Background(), engine.Event{Seq: 1, Type: engine.EventModelDelta,
		Data: engine.EncodeData(engine.ModelDelta{Content: f.result.Content})}); err != nil {
		return nil, err
	}
	return f.result, nil
}

func TestConversationRunTurn(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()
	agents := agent.NewPostgresStore(pool)
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	v1 := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: `{"system_prompt":"hi","model_profile_version_id":"m1"}`, CreatedBy: "owner"}
	if err := agents.CreateAgentWithInitialVersion(ctx, a, v1); err != nil {
		t.Fatal(err)
	}
	agentSvc := agent.NewService(agents, nil, nil)
	store := NewPostgresStore(pool)
	conv := &domain.Conversation{ID: uuid.NewString(), WorkspaceID: "space-a", AgentID: a.ID, AgentVersionID: v1.ID, UserID: "user-1"}
	if err := store.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}

	runner := fakeRunner{result: &engine.RunResult{Content: "hello", Model: "m", Usage: llm.Usage{TotalTokens: 3}, NextSeq: 1}}
	svc := NewService(store, agentSvc, nil, runner)
	emit := &captureEmitter{}
	reply, err := svc.RunTurn(ctx, ChatTurnRequest{WorkspaceID: "space-a", UserID: "user-1", ConversationID: conv.ID, Input: "hi"}, emit)
	if err != nil || reply.Role != "assistant" || reply.Content != "hello" {
		t.Fatalf("run turn = %+v, %v", reply, err)
	}
	if len(emit.events) != 2 || emit.events[0].Type != engine.EventModelDelta || emit.events[1].Type != engine.EventRunCompleted || emit.events[1].Seq != 2 {
		t.Fatalf("events = %+v", emit.events)
	}
	messages, err := store.ListMessages(ctx, "space-a", conv.ID)
	if err != nil || len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("messages = %+v, %v", messages, err)
	}
	if got, err := store.GetConversation(ctx, "space-a", conv.ID); err != nil || got.Status != "active" {
		t.Fatalf("conversation = %+v, %v", got, err)
	}
	probeToken, _, err := store.ClaimTurn(ctx, "space-a", conv.ID, 30)
	if err != nil {
		t.Fatalf("lease must be released after commit: %v", err)
	}
	if err := store.ReleaseTurn(ctx, "space-a", conv.ID, probeToken, "active"); err != nil {
		t.Fatal(err)
	}
	_ = store.ReleaseTurn(ctx, "space-a", conv.ID, "invalid", "active")

	if _, err := svc.RunTurn(ctx, ChatTurnRequest{WorkspaceID: "space-a", UserID: "user-2", ConversationID: conv.ID, Input: "hi"}, &captureEmitter{}); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("other user turn = %v", err)
	}
	if _, err := svc.RunTurn(ctx, ChatTurnRequest{WorkspaceID: "space-a", UserID: "user-1", ConversationID: conv.ID, Input: " "}, &captureEmitter{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("blank input = %v", err)
	}

	token, _, err := store.ClaimTurn(ctx, "space-a", conv.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunTurn(ctx, ChatTurnRequest{WorkspaceID: "space-a", UserID: "user-1", ConversationID: conv.ID, Input: "hi"}, &captureEmitter{}); !errors.Is(err, ErrTurnBusy) {
		t.Fatalf("busy turn = %v", err)
	}
	if err := store.ReleaseTurn(ctx, "space-a", conv.ID, token, "active"); err != nil {
		t.Fatal(err)
	}

	failing := NewService(store, agentSvc, nil, fakeRunner{err: errors.New("model down")})
	if _, err := failing.RunTurn(ctx, ChatTurnRequest{WorkspaceID: "space-a", UserID: "user-1", ConversationID: conv.ID, Input: "again"}, &captureEmitter{}); err == nil {
		t.Fatal("runner failure accepted")
	}
	if messages, err := store.ListMessages(ctx, "space-a", conv.ID); err != nil || len(messages) != 2 {
		t.Fatalf("failed turn must not save messages: %+v, %v", messages, err)
	}
	if _, _, err := store.ClaimTurn(ctx, "space-a", conv.ID, 30); err != nil {
		t.Fatalf("failed turn must release the lease: %v", err)
	}

	if _, err := NewService(store, agentSvc, nil, nil).RunTurn(ctx, ChatTurnRequest{WorkspaceID: "space-a", UserID: "user-1", ConversationID: conv.ID, Input: "hi"}, &captureEmitter{}); err == nil {
		t.Fatal("missing executor accepted")
	}
}

type historyRecordingGateway struct {
	llm.Gateway
	requests []llm.GenerateRequest
}

func (g *historyRecordingGateway) Stream(_ context.Context, _, _ string, req llm.GenerateRequest) (llm.Stream, error) {
	g.requests = append(g.requests, req)
	return historyReplyStream{}, nil
}

type historyReplyStream struct{}

func (historyReplyStream) Recv(context.Context) (*llm.StreamChunk, error) {
	return &llm.StreamChunk{Content: "已记录", Done: true}, nil
}

func (historyReplyStream) Close() error { return nil }

func TestConversationRunTurnReplaysHistory(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()
	agents := agent.NewPostgresStore(pool)
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	v := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: `{}`, CreatedBy: "owner"}
	if err := agents.CreateAgentWithInitialVersion(ctx, a, v); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	gateway := &historyRecordingGateway{}
	svc := NewService(store, nil, nil, engine.NewExecutor(nil, gateway))

	createConversation := func(t *testing.T) *domain.Conversation {
		t.Helper()
		conv := &domain.Conversation{
			ID: uuid.NewString(), WorkspaceID: a.WorkspaceID, AgentID: a.ID,
			AgentVersionID: v.ID, UserID: "user-1", RuntimeConfigJSON: `{"system_prompt":"你是客服"}`,
		}
		if err := store.CreateConversation(ctx, conv); err != nil {
			t.Fatal(err)
		}
		return conv
	}
	run := func(t *testing.T, conv *domain.Conversation, input string) llm.GenerateRequest {
		t.Helper()
		count := len(gateway.requests)
		_, err := svc.RunTurn(ctx, ChatTurnRequest{
			WorkspaceID: conv.WorkspaceID, UserID: conv.UserID, ConversationID: conv.ID, Input: input,
		}, &captureEmitter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(gateway.requests) != count+1 {
			t.Fatalf("model requests = %d, want %d", len(gateway.requests), count+1)
		}
		return gateway.requests[count]
	}
	assertMessages := func(t *testing.T, req llm.GenerateRequest, want []llm.Message) {
		t.Helper()
		if req.SystemPrompt != "你是客服" || !reflect.DeepEqual(req.Messages, want) {
			t.Fatalf("system = %q, model messages = %+v, want %+v", req.SystemPrompt, req.Messages, want)
		}
	}

	t.Run("second turn", func(t *testing.T) {
		conv := createConversation(t)
		first := llm.Message{Role: "user", Content: "我的订单号是 ORD-42"}
		second := llm.Message{Role: "user", Content: "刚才的订单号是多少？"}
		assertMessages(t, run(t, conv, first.Content), []llm.Message{first})
		assertMessages(t, run(t, conv, second.Content), []llm.Message{
			first, {Role: "assistant", Content: "已记录"}, second,
		})
	})

	t.Run("latest ten", func(t *testing.T) {
		conv := createConversation(t)
		token, _, err := store.ClaimTurn(ctx, conv.WorkspaceID, conv.ID, 60)
		if err != nil {
			t.Fatal(err)
		}
		var stored []*domain.Message
		var expected []llm.Message
		for i := range 12 {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			content := strconv.Itoa(i)
			stored = append(stored, &domain.Message{
				ID: uuid.NewString(), ConversationID: conv.ID, Role: role, Content: content,
			})
			expected = append(expected, llm.Message{Role: role, Content: content})
		}
		if err := store.CommitTurn(ctx, conv.WorkspaceID, conv.ID, token, stored, "active", nil); err != nil {
			t.Fatal(err)
		}
		current := llm.Message{Role: "user", Content: "新问题"}
		want := append(expected[2:], current)
		assertMessages(t, run(t, conv, current.Content), want)
	})
}

type userTemplateStore struct {
	prompt.Store
	versions map[string]*prompt.PromptVersion
}

func (s userTemplateStore) GetPromptVersion(_ context.Context, workspaceID, versionID string) (*prompt.PromptVersion, error) {
	version := s.versions[versionID]
	if workspaceID != "space-a" || version == nil {
		return nil, prompt.ErrPromptVersionNotFound
	}
	return version, nil
}

func seedChatPromptVersion(t *testing.T, pool *pgxpool.Pool, workspaceID string, version *prompt.PromptVersion, number int) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, $1) ON CONFLICT DO NOTHING", workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO prompts (id, workspace_id, name) VALUES ($1, $2, 'Chat') ON CONFLICT DO NOTHING",
		uuid.MustParse(version.PromptID), workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO prompt_versions (id, prompt_id, version, template) VALUES ($1, $2, $3, $4)",
		uuid.MustParse(version.ID), uuid.MustParse(version.PromptID), number, version.Template); err != nil {
		t.Fatal(err)
	}
}

func TestConversationFirstTurnUserTemplate(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	v := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: `{}`, CreatedBy: "owner"}
	if err := agent.NewPostgresStore(pool).CreateAgentWithInitialVersion(ctx, a, v); err != nil {
		t.Fatal(err)
	}
	template := &prompt.PromptVersion{
		ID: uuid.NewString(), PromptID: uuid.NewString(), Template: "核对订单 {{.order_id}}",
		VariablesSchema: `{"required":["order_id"]}`,
	}
	seedChatPromptVersion(t, pool, a.WorkspaceID, template, 1)
	store := NewPostgresStore(pool)
	gateway := &historyRecordingGateway{}
	renderer := prompt.NewService(userTemplateStore{versions: map[string]*prompt.PromptVersion{template.ID: template}}, nil)
	svc := NewService(store, nil, nil, engine.NewExecutor(renderer, gateway))

	for _, tt := range []struct {
		name  string
		input string
		vars  map[string]any
		want  string
	}{
		{name: "plain text with a bound template", input: "你好", want: "你好"},
		{name: "vars only", vars: map[string]any{"order_id": "ORD-42"}, want: "核对订单 ORD-42"},
		{name: "vars and supplemental input", input: "  用中文回答  ", vars: map[string]any{"order_id": "ORD-42"}, want: "核对订单 ORD-42\n\n# 补充说明\n\n用中文回答"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conv := &domain.Conversation{
				ID: uuid.NewString(), WorkspaceID: a.WorkspaceID, AgentID: a.ID, AgentVersionID: v.ID,
				UserID: "user-1", RuntimeConfigJSON: `{"user_prompt_version_id":"` + template.ID + `","user_prompt_experiment":"exp-1","user_prompt_variant":"candidate"}`,
			}
			if err := store.CreateConversation(ctx, conv); err != nil {
				t.Fatal(err)
			}
			req := ChatTurnRequest{WorkspaceID: a.WorkspaceID, UserID: conv.UserID, ConversationID: conv.ID}
			count := len(gateway.requests)
			req.Vars = map[string]any{}
			if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); !errors.Is(err, prompt.ErrInvalidArgument) {
				t.Fatalf("missing template variable = %v", err)
			}
			req.Input, req.Vars = tt.input, tt.vars
			if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); err != nil {
				t.Fatal(err)
			}
			if len(gateway.requests) != count+1 {
				t.Fatalf("model requests after invalid and valid turn = %d", len(gateway.requests)-count)
			}
			if got := gateway.requests[count].Messages; len(got) != 1 || got[0].Content != tt.want {
				t.Fatalf("first sent message = %+v, want %q", got, tt.want)
			}
			if tt.vars != nil {
				saved, err := store.GetConversation(ctx, conv.WorkspaceID, conv.ID)
				if err != nil {
					t.Fatal(err)
				}
				var snapshot domain.UserPromptSnapshot
				if err := json.Unmarshal([]byte(saved.RuntimeConfigJSON), &snapshot); err != nil {
					t.Fatal(err)
				}
				if snapshot.ExperimentID != "exp-1" || snapshot.ExperimentVariant != "candidate" || snapshot.Rendered != tt.want {
					t.Fatalf("saved default experiment attribution = %+v", snapshot)
				}
			}
			req.Input, req.Vars = "继续", map[string]any{}
			if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("unexpected vars on later turn = %v", err)
			}
			req.Vars = nil
			if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); err != nil {
				t.Fatal(err)
			}
			if len(gateway.requests) != count+2 {
				t.Fatalf("model requests = %d, want %d", len(gateway.requests), count+2)
			}
			if got := gateway.requests[count+1].Messages; len(got) != 3 || got[0].Content != tt.want || got[2].Content != "继续" {
				t.Fatalf("continued turn model messages = %+v", got)
			}
			messages, err := store.ListMessages(ctx, a.WorkspaceID, conv.ID)
			if err != nil || len(messages) != 4 || messages[0].Content != tt.want {
				t.Fatalf("persisted messages = %+v, %v", messages, err)
			}
		})
	}
}

func TestConversationSelectsFirstTurnTemplateVersion(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()
	agents := agent.NewPostgresStore(pool)
	promptID := uuid.NewString()
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	v := &domain.AgentVersion{
		ID: uuid.NewString(), CreatedBy: "owner",
		SnapshotJSON: `{"user_prompt_id":"` + promptID + `"}`,
	}
	if err := agents.CreateAgentWithInitialVersion(ctx, a, v); err != nil {
		t.Fatal(err)
	}
	defaultVersion := &prompt.PromptVersion{ID: uuid.NewString(), PromptID: promptID, Template: "default"}
	chosen := &prompt.PromptVersion{ID: uuid.NewString(), PromptID: promptID, Template: "查询 {{.order_id}}"}
	otherPrompt := &prompt.PromptVersion{ID: uuid.NewString(), PromptID: uuid.NewString(), Template: "wrong"}
	seedChatPromptVersion(t, pool, a.WorkspaceID, chosen, 2)
	renderer := prompt.NewService(userTemplateStore{versions: map[string]*prompt.PromptVersion{
		defaultVersion.ID: defaultVersion, chosen.ID: chosen, otherPrompt.ID: otherPrompt,
	}}, nil)
	store := NewPostgresStore(pool)
	gateway := &historyRecordingGateway{}
	svc := NewService(store, agent.NewService(agents, nil, nil), nil, engine.NewExecutor(renderer, gateway))
	conv := &domain.Conversation{
		ID: uuid.NewString(), WorkspaceID: a.WorkspaceID,
		AgentID: a.ID, AgentVersionID: v.ID, UserID: "user-1",
		RuntimeConfigJSON: `{"system_prompt":"客服指令","user_prompt_version_id":"` + defaultVersion.ID + `","user_prompt_experiment":"exp-1","user_prompt_variant":"candidate"}`,
	}
	if err := store.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	req := ChatTurnRequest{
		WorkspaceID: a.WorkspaceID, UserID: conv.UserID, ConversationID: conv.ID,
		Input: "普通文本", UserPromptVersionID: chosen.ID,
	}
	if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("version without vars = %v", err)
	}
	req.Input = ""
	req.Vars = map[string]any{"order_id": "ORD-42"}
	for _, tc := range []struct {
		id  string
		err error
	}{
		{otherPrompt.ID, prompt.ErrPromptVersionNotFound},
		{uuid.NewString(), prompt.ErrPromptVersionNotFound},
		{"invalid-uuid", prompt.ErrInvalidArgument},
	} {
		req.UserPromptVersionID = tc.id
		if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); !errors.Is(err, tc.err) {
			t.Fatalf("reject selected version %q: %v", tc.id, err)
		}
	}
	if len(gateway.requests) != 0 {
		t.Fatalf("invalid selection reached the model %d times", len(gateway.requests))
	}
	req.UserPromptVersionID = " " + chosen.ID + " "
	events := &captureEmitter{}
	if _, err := svc.RunTurn(ctx, req, events); err != nil {
		t.Fatal(err)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("model requests = %d", len(gateway.requests))
	}
	if got := gateway.requests[0].Messages; len(got) != 1 || got[0].Content != "查询 ORD-42" {
		t.Fatalf("model messages = %+v", got)
	}
	var resolved engine.PromptResolved
	if len(events.events) < 2 || events.events[1].Type != engine.EventPromptResolved {
		t.Fatalf("expected prompt_resolved, got %+v", events.events)
	}
	if err := json.Unmarshal(events.events[1].Data, &resolved); err != nil || resolved.UserVersionID != chosen.ID {
		t.Fatalf("selected version in event = %+v, %v", resolved, err)
	}
	if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("later version selection = %v", err)
	}
	req.Input, req.Vars, req.UserPromptVersionID = "继续", nil, ""
	if _, err := svc.RunTurn(ctx, req, &captureEmitter{}); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetConversation(ctx, a.WorkspaceID, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gateway.requests[1].SystemPrompt != "客服指令" {
		t.Fatalf("system prompt lost: %q", gateway.requests[1].SystemPrompt)
	}
	var snapshot domain.UserPromptSnapshot
	if err := json.Unmarshal([]byte(saved.RuntimeConfigJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.VersionID != chosen.ID || snapshot.Rendered != "查询 ORD-42" ||
		snapshot.ExperimentID != "" || snapshot.ExperimentVariant != "" ||
		!reflect.DeepEqual(snapshot.Variables, map[string]any{"order_id": "ORD-42"}) {
		t.Fatalf("saved user prompt snapshot = %+v", snapshot)
	}

}

type chatRunnerFunc func(context.Context, engine.RunInput, engine.Emitter) (*engine.RunResult, error)

func (f chatRunnerFunc) Run(ctx context.Context, in engine.RunInput, emit engine.Emitter) (*engine.RunResult, error) {
	return f(ctx, in, emit)
}

type failingRenewalStore struct{ Store }

func (failingRenewalStore) RenewTurn(context.Context, string, string, string, int32) error {
	return ErrTurnLost
}

type delayedCommitStore struct {
	Store
	started chan struct{}
	proceed chan struct{}
}

func (s delayedCommitStore) CommitTurn(ctx context.Context, workspaceID, conversationID, token string, messages []*domain.Message, status string, snapshot []byte) error {
	close(s.started)
	select {
	case <-s.proceed:
		return s.Store.CommitTurn(ctx, workspaceID, conversationID, token, messages, status, snapshot)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestConversationLongTurnRenewsAndSyncCollectsDeltas(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	agentStore := agent.NewPostgresStore(pool)
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	version := &domain.AgentVersion{ID: uuid.NewString(), CreatedBy: "owner", SnapshotJSON: `{}`}
	if err := agentStore.CreateAgentWithInitialVersion(ctx, a, version); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	conv := &domain.Conversation{
		ID: uuid.NewString(), WorkspaceID: a.WorkspaceID, AgentID: a.ID, AgentVersionID: version.ID, UserID: "user-1",
	}
	if err := store.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	req := ChatTurnRequest{WorkspaceID: a.WorkspaceID, ConversationID: conv.ID, UserID: conv.UserID, Input: "hello"}
	started, release := make(chan engine.RunInput, 1), make(chan struct{})
	commit := delayedCommitStore{Store: store, started: make(chan struct{}), proceed: make(chan struct{})}
	svc := NewService(commit, nil, nil, chatRunnerFunc(func(ctx context.Context, in engine.RunInput, emit engine.Emitter) (*engine.RunResult, error) {
		started <- in
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		for i, delta := range []string{"hel", "lo"} {
			if err := emit.Emit(ctx, engine.Event{Seq: int64(i + 1), Type: engine.EventModelDelta,
				Data: engine.EncodeData(engine.ModelDelta{Content: delta})}); err != nil {
				return nil, err
			}
		}
		return &engine.RunResult{Content: "hello", NextSeq: 2}, nil
	}))
	svc.turnLeaseSeconds = 1
	svc.turnRenewInterval = 100 * time.Millisecond

	type syncOutcome struct {
		content string
		err     error
	}
	outcome := make(chan syncOutcome, 1)
	go func() {
		content, err := svc.Chat(ctx, req)
		outcome <- syncOutcome{content, err}
	}()
	select {
	case input := <-started:
		if input.AgentVersionID != version.ID || input.TurnRevision != 1 {
			t.Fatalf("runner input = %+v", input)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Wait beyond the original lease window; another request must still be rejected.
	time.Sleep(1200 * time.Millisecond)
	if _, _, err := store.ClaimTurn(ctx, conv.WorkspaceID, conv.ID, 60); !errors.Is(err, ErrTurnBusy) {
		t.Fatalf("renewed turn could be taken over: %v", err)
	}
	close(release)
	select {
	case <-commit.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	time.Sleep(1200 * time.Millisecond)
	if _, _, err := store.ClaimTurn(ctx, conv.WorkspaceID, conv.ID, 60); !errors.Is(err, ErrTurnBusy) {
		t.Fatalf("turn could be taken over during persistence: %v", err)
	}
	close(commit.proceed)
	select {
	case got := <-outcome:
		if got.err != nil || got.content != "hello" {
			t.Fatalf("synchronous chat = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	messages, err := store.ListMessages(ctx, conv.WorkspaceID, conv.ID)
	if err != nil || len(messages) != 2 || messages[1].Content != "hello" {
		t.Fatalf("saved messages = %+v, %v", messages, err)
	}

	broken := NewService(failingRenewalStore{store}, nil, nil, chatRunnerFunc(func(ctx context.Context, _ engine.RunInput, emit engine.Emitter) (*engine.RunResult, error) {
		if err := emit.Emit(ctx, engine.Event{Seq: 1, Type: engine.EventRunStarted}); err != nil {
			return nil, err
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	broken.turnRenewInterval = 20 * time.Millisecond
	events := &captureEmitter{}
	if _, err := broken.RunTurn(ctx, req, events); !errors.Is(err, ErrTurnLost) {
		t.Fatalf("lost lease did not cancel the run: %v", err)
	}
	if len(events.events) != 2 || events.events[1].Type != engine.EventRunFailed ||
		!strings.Contains(string(events.events[1].Data), "turn_lease_lost") {
		t.Fatalf("lost lease events = %+v", events.events)
	}
	if history, err := store.ListMessages(ctx, conv.WorkspaceID, conv.ID); err != nil || len(history) != 2 {
		t.Fatalf("failed renewal appended messages: %+v, %v", history, err)
	}
	probe, _, err := store.ClaimTurn(ctx, conv.WorkspaceID, conv.ID, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseTurn(ctx, conv.WorkspaceID, conv.ID, probe, "active"); err != nil {
		t.Fatal(err)
	}
}
