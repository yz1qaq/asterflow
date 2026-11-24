package conversation

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
)

func newConversationTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for isolated conversation tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("connect to explicit conversation test PostgreSQL URL")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_conv_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated conversation schema: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("parse explicit conversation test PostgreSQL URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create conversation test pool")
	}
	t.Cleanup(pool.Close)
	for _, name := range []string{
		"000001_init_iam.up.sql", "000003_model_accounts_deployments.up.sql", "000004_model_profiles.up.sql",
		"000006_prompt_versions.up.sql", "000009_agents.up.sql", "000010_conversations.up.sql", "000011_prompt_draft_deletion.up.sql",
	} {
		text, err := os.ReadFile("../../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(text)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}

func TestConversationStoreAndTurnLease(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()
	agents := agent.NewPostgresStore(pool)
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	v1 := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: `{"system_prompt":"hi"}`, CreatedBy: "owner"}
	if err := agents.CreateAgentWithInitialVersion(ctx, a, v1); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)

	conv := &domain.Conversation{ID: uuid.NewString(), WorkspaceID: "space-a", AgentID: a.ID,
		AgentVersionID: v1.ID, UserID: "user-1"}
	if err := store.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	if conv.Status != "active" || conv.Classification != "internal" || conv.StartedAt.IsZero() {
		t.Fatalf("created conversation = %+v", conv)
	}
	if err := store.CreateConversation(ctx, &domain.Conversation{ID: uuid.NewString(), WorkspaceID: "space-a",
		AgentID: a.ID, AgentVersionID: uuid.NewString(), UserID: "user-1"}); err == nil {
		t.Fatal("conversation must pin a version of its own agent")
	}
	got, err := store.GetConversation(ctx, "space-a", conv.ID)
	if err != nil || got.UserID != "user-1" {
		t.Fatalf("get conversation = %+v, %v", got, err)
	}
	if _, err := store.GetConversation(ctx, "space-b", conv.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("cross-space conversation = %v", err)
	}
	if list, err := store.ListConversations(ctx, "space-a", "user-1", 10, 0); err != nil || len(list) != 1 {
		t.Fatalf("owner list = %+v, %v", list, err)
	}
	if list, err := store.ListConversations(ctx, "space-a", "user-2", 10, 0); err != nil || len(list) != 0 {
		t.Fatalf("other user list = %+v, %v", list, err)
	}

	token, revision, err := store.ClaimTurn(ctx, "space-a", conv.ID, 30)
	if err != nil || token == "" || revision != 1 {
		t.Fatalf("claim = %q %d %v", token, revision, err)
	}
	if _, _, err := store.ClaimTurn(ctx, "space-a", conv.ID, 30); !errors.Is(err, ErrTurnBusy) {
		t.Fatalf("second claim = %v", err)
	}

	callID := uuid.NewString()
	message := &domain.Message{ID: uuid.NewString(), ConversationID: conv.ID, Role: "assistant", Content: "done",
		ToolCalls: []domain.ToolCall{{ID: callID, ToolID: uuid.NewString(), Args: `{"q":"x"}`, Result: `{"ok":true}`, LatencyMs: 7}}}
	if err := store.CommitTurn(ctx, "space-a", conv.ID, token, []*domain.Message{message}, "completed", nil); err != nil {
		t.Fatal(err)
	}
	messages, err := store.ListMessages(ctx, "space-a", conv.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "done" || len(messages[0].ToolCalls) != 1 {
		t.Fatalf("messages = %+v, %v", messages, err)
	}
	got, err = store.GetConversation(ctx, "space-a", conv.ID)
	if err != nil || got.Status != "completed" {
		t.Fatalf("committed conversation = %+v, %v", got, err)
	}
	if err := store.CommitTurn(ctx, "space-a", conv.ID, token, []*domain.Message{{ID: uuid.NewString(), ConversationID: conv.ID, Role: "assistant", Content: "again"}}, "completed", []byte(`{"rendered_user_prompt":"should roll back"}`)); !errors.Is(err, ErrTurnLost) {
		t.Fatalf("stale commit = %v", err)
	}
	if messages, err := store.ListMessages(ctx, "space-a", conv.ID); err != nil || len(messages) != 1 {
		t.Fatalf("failed commit must roll back messages: %+v, %v", messages, err)
	}

	token, _, err = store.ClaimTurn(ctx, "space-a", conv.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseTurn(ctx, "space-a", conv.ID, token, "active"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseTurn(ctx, "space-a", conv.ID, token, "active"); !errors.Is(err, ErrTurnLost) {
		t.Fatalf("second release = %v", err)
	}
	if got, err := store.GetConversation(ctx, "space-a", conv.ID); err != nil || got.Status != "active" {
		t.Fatalf("released conversation = %+v, %v", got, err)
	}

	token, _, err = store.ClaimTurn(ctx, "space-a", conv.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, _, err := store.ClaimTurn(ctx, "space-a", conv.ID, 30); err != nil {
		t.Fatalf("expired lease must be claimable: %v", err)
	}
}

func TestConversationSnapshotRollsBackWithMessages(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	v := &domain.AgentVersion{ID: uuid.NewString(), CreatedBy: "owner", SnapshotJSON: `{}`}
	if err := agent.NewPostgresStore(pool).CreateAgentWithInitialVersion(ctx, a, v); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	conv := &domain.Conversation{
		ID: uuid.NewString(), WorkspaceID: a.WorkspaceID, AgentID: a.ID,
		AgentVersionID: v.ID, UserID: "user-1", RuntimeConfigJSON: `{"system_prompt":"keep"}`,
	}
	if err := store.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.ClaimTurn(ctx, conv.WorkspaceID, conv.ID, 60)
	if err != nil {
		t.Fatal(err)
	}
	messages := []*domain.Message{{
		ID: uuid.NewString(), ConversationID: conv.ID, Role: "user", Content: "should not persist",
	}}
	if err := store.CommitTurn(ctx, conv.WorkspaceID, conv.ID, uuid.NewString(), messages, "active",
		[]byte(`{"rendered_user_prompt":"should not persist"}`)); !errors.Is(err, ErrTurnLost) {
		t.Fatalf("stale token commit = %v", err)
	}
	saved, err := store.GetConversation(ctx, conv.WorkspaceID, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.RuntimeConfigJSON, "rendered_user_prompt") || !strings.Contains(saved.RuntimeConfigJSON, "keep") {
		t.Fatalf("stale token touched the runtime snapshot: %s", saved.RuntimeConfigJSON)
	}
	if got, err := store.ListMessages(ctx, conv.WorkspaceID, conv.ID); err != nil || len(got) != 0 {
		t.Fatalf("stale token inserted messages: %+v, %v", got, err)
	}
}
