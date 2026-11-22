package conversation

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
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
