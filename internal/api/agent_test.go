package api

import (
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
)

func TestAgentHTTP(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	ctx := context.Background()
	for _, migration := range []string{
		"000003_model_accounts_deployments.up.sql", "000004_model_profiles.up.sql",
		"000005_project_model_bindings.up.sql", "000006_prompt_versions.up.sql",
		"000009_agents.up.sql", "000010_conversations.up.sql", "000011_prompt_draft_deletion.up.sql",
	} {
		script, err := os.ReadFile("../../migrations/" + migration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(script)); err != nil {
			t.Fatal(err)
		}
	}

	identities := iam.NewService(iam.NewPostgresStore(pool), []byte("agent-http-fixture-key-at-least-32-bytes"))
	createUser := func(email, name string) *domain.User {
		t.Helper()
		user, err := identities.CreateUser(ctx, email, "fixture-password123", name)
		if err != nil {
			t.Fatal(err)
		}
		return user
	}
	owner := createUser("owner@agent.test", "Owner")
	viewer := createUser("viewer@agent.test", "Viewer")
	editor := createUser("editor@agent.test", "Editor")
	makeSpace := func(name string) string {
		t.Helper()
		ws, err := identities.CreateWorkspaceForUser(ctx, owner.ID, name, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return ws.ID
	}
	a, b := makeSpace("A"), makeSpace("B")
	if _, err := identities.UpsertWorkspaceMember(ctx, owner.ID, a, viewer.ID, iam.WorkspaceRoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := identities.UpsertWorkspaceMember(ctx, owner.ID, a, editor.ID, iam.WorkspaceRoleEditor); err != nil {
		t.Fatal(err)
	}
	tokenFor := func(email string) string {
		t.Helper()
		res, err := identities.Login(ctx, iam.LoginRequest{Email: email, Password: "fixture-password123"})
		if err != nil {
			t.Fatal(err)
		}
		return res.Token
	}
	ownToken, viewerToken, editorToken := tokenFor(owner.Email), tokenFor(viewer.Email), tokenFor(editor.Email)

	models := modelconfig.NewService(modelconfig.NewPostgresStore(pool), nil)
	prompts := prompt.NewService(prompt.NewPostgresStore(pool), models)
	agents := agent.NewService(agent.NewPostgresStore(pool), prompts, models)
	conversationStore := conversation.NewPostgresStore(pool)
	conversations := conversation.NewService(conversationStore, agents, prompts, nil)
	server := httptest.NewServer(NewRouter(pool, identities, models, prompts, Services{Agent: agents, Conversation: conversations}))
	defer server.Close()

	call := func(method, path, body, token, space string, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if space != "" {
			req.Header.Set("X-Workspace-ID", space)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		got, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d err=%v body=%s", method, path, res.StatusCode, want, err, got)
		}
		return got
	}
	marshal := func(v any) string {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	var createdPrompt struct {
		Prompt  prompt.Prompt        `json:"prompt"`
		Version prompt.PromptVersion `json:"version"`
	}
	if err := json.Unmarshal(call("POST", "/api/v1/prompts", marshal(map[string]any{
		"name": "Support", "template": "你是一个客服 {{.order_id}}",
	}), ownToken, a, http.StatusCreated), &createdPrompt); err != nil {
		t.Fatal(err)
	}

	base := "/api/v1/agents"
	call("POST", base, marshal(map[string]any{
		"name": "literal and pinned", "system_prompt": "hi", "system_prompt_version_id": createdPrompt.Version.ID,
	}), ownToken, a, http.StatusBadRequest)
	call("POST", base, marshal(map[string]any{
		"name": "bad ref", "system_prompt_version_id": uuid.NewString(),
	}), ownToken, a, http.StatusNotFound)
	call("POST", base, marshal(map[string]any{"name": "Chatty"}), viewerToken, a, http.StatusForbidden)

	zero := float32(0)
	var created struct {
		Agent   domain.Agent        `json:"agent"`
		Version domain.AgentVersion `json:"version"`
	}
	if err := json.Unmarshal(call("POST", base, marshal(map[string]any{
		"name": "Support Agent", "description": "d", "template": "customer_support",
		"system_prompt_version_id": createdPrompt.Version.ID,
		"prompt_env":               "dev",
		"generation_config":        map[string]any{"temperature": zero},
	}), ownToken, a, http.StatusCreated), &created); err != nil {
		t.Fatal(err)
	}
	if created.Version.Version != 1 || created.Agent.WorkspaceID != a || created.Version.AgentID != created.Agent.ID {
		t.Fatalf("created = %+v", created)
	}
	if !strings.Contains(created.Version.SnapshotJSON, createdPrompt.Version.ID) || !strings.Contains(created.Version.SnapshotJSON, `"temperature":0`) {
		t.Fatalf("snapshot = %s", created.Version.SnapshotJSON)
	}

	var list []domain.Agent
	if err := json.Unmarshal(call("GET", base, "", viewerToken, a, http.StatusOK), &list); err != nil || len(list) != 1 || list[0].ID != created.Agent.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}
	var fetched domain.Agent
	if err := json.Unmarshal(call("GET", base+"/"+created.Agent.ID, "", ownToken, a, http.StatusOK), &fetched); err != nil || fetched.Name != "Support Agent" || fetched.Description != "d" || fetched.Template != "customer_support" {
		t.Fatalf("get = %+v, %v", fetched, err)
	}
	call("GET", base+"/"+created.Agent.ID, "", ownToken, b, http.StatusNotFound)
	call("GET", base+"/invalid", "", ownToken, a, http.StatusBadRequest)

	var v2 domain.AgentVersion
	if err := json.Unmarshal(call("POST", base+"/"+created.Agent.ID+"/versions", marshal(map[string]any{
		"system_prompt": "you are concise",
	}), ownToken, a, http.StatusCreated), &v2); err != nil || v2.Version != 2 {
		t.Fatalf("append = %+v, %v", v2, err)
	}
	call("POST", base+"/"+created.Agent.ID+"/versions", marshal(map[string]any{"system_prompt": "x"}), viewerToken, a, http.StatusForbidden)
	if err := json.Unmarshal(call("GET", base+"/"+created.Agent.ID+"/versions", "", viewerToken, a, http.StatusOK), &[]domain.AgentVersion{}); err != nil {
		t.Fatal(err)
	}

	var envs []agent.EnvBinding
	if err := json.Unmarshal(call("GET", base+"/"+created.Agent.ID+"/envs", "", viewerToken, a, http.StatusOK), &envs); err != nil || len(envs) != 1 || envs[0].Env != "dev" || envs[0].VersionID != created.Version.ID {
		t.Fatalf("envs = %+v, %v", envs, err)
	}
	call("POST", base+"/"+created.Agent.ID+"/promote", marshal(map[string]any{
		"env": "dev", "version_id": v2.ID,
	}), viewerToken, a, http.StatusForbidden)
	call("POST", base+"/"+created.Agent.ID+"/promote", marshal(map[string]any{
		"env": "dev", "version_id": "v2",
	}), ownToken, a, http.StatusBadRequest)
	if raw := call("POST", base+"/"+created.Agent.ID+"/promote", marshal(map[string]any{
		"env": "dev", "version_id": v2.ID,
	}), ownToken, a, http.StatusOK); string(raw) != `{"status":"promoted"}` {
		t.Fatalf("promote response = %s", raw)
	}
	envs = nil
	if err := json.Unmarshal(call("GET", base+"/"+created.Agent.ID+"/envs", "", ownToken, a, http.StatusOK), &envs); err != nil || len(envs) != 1 || envs[0].VersionID != v2.ID {
		t.Fatalf("promoted envs = %+v, %v", envs, err)
	}
	call("POST", base+"/"+created.Agent.ID+"/promote", marshal(map[string]any{
		"env": "dev", "version_id": v2.ID,
	}), ownToken, b, http.StatusNotFound)

	target := base + "/" + created.Agent.ID
	call("DELETE", target, "", "", a, http.StatusUnauthorized)
	call("DELETE", target, "", viewerToken, a, http.StatusForbidden)
	call("DELETE", target, "", editorToken, a, http.StatusForbidden)
	call("DELETE", target, "", ownToken, b, http.StatusNotFound)
	call("DELETE", base+"/invalid", "", ownToken, a, http.StatusBadRequest)

	newConversation := func(agentID, versionID, token string) domain.Conversation {
		t.Helper()
		var conv domain.Conversation
		err := json.Unmarshal(call("POST", "/api/v1/conversations", marshal(map[string]string{
			"agent_id": agentID, "agent_version_id": versionID,
		}), token, a, http.StatusCreated), &conv)
		if err != nil {
			t.Fatal(err)
		}
		return conv
	}
	firstTurn := newConversation(created.Agent.ID, created.Version.ID, ownToken)
	secondTurn := newConversation(created.Agent.ID, v2.ID, editorToken)

	var other struct {
		Agent   domain.Agent
		Version domain.AgentVersion
	}
	if err := json.Unmarshal(call("POST", base, `{"name":"Unrelated Agent"}`, ownToken, a, http.StatusCreated), &other); err != nil {
		t.Fatal(err)
	}
	keep := newConversation(other.Agent.ID, other.Version.ID, ownToken)

	token, _, err := conversationStore.ClaimTurn(ctx, a, firstTurn.ID, 60)
	if err != nil {
		t.Fatal(err)
	}
	messageID, toolCallID := uuid.NewString(), uuid.NewString()
	if err := conversationStore.CommitTurn(ctx, a, firstTurn.ID, token, []*domain.Message{{
		ID: messageID, ConversationID: firstTurn.ID, Role: "assistant",
		ToolCalls: []domain.ToolCall{{ID: toolCallID, Args: "{}"}},
	}}, "active", nil); err != nil {
		t.Fatal(err)
	}
	busyToken, _, err := conversationStore.ClaimTurn(ctx, a, secondTurn.ID, 60)
	if err != nil {
		t.Fatal(err)
	}
	call("DELETE", target, "", ownToken, a, http.StatusConflict)
	call("GET", target, "", ownToken, a, http.StatusOK)
	call("GET", "/api/v1/conversations/"+firstTurn.ID, "", ownToken, a, http.StatusOK)
	call("GET", "/api/v1/conversations/"+secondTurn.ID, "", editorToken, a, http.StatusOK)
	if err := conversationStore.ReleaseTurn(ctx, a, secondTurn.ID, busyToken, "active"); err != nil {
		t.Fatal(err)
	}
	if body := call("DELETE", target, "", ownToken, a, http.StatusNoContent); len(body) != 0 {
		t.Fatalf("expected an empty success response: %s", body)
	}
	call("DELETE", target, "", ownToken, a, http.StatusNotFound)
	call("GET", target+"/versions", "", ownToken, a, http.StatusNotFound)
	call("GET", target+"/envs", "", ownToken, a, http.StatusNotFound)
	call("GET", "/api/v1/conversations/"+firstTurn.ID, "", ownToken, a, http.StatusNotFound)
	call("GET", "/api/v1/conversations/"+secondTurn.ID, "", editorToken, a, http.StatusNotFound)
	call("GET", "/api/v1/conversations/"+keep.ID, "", ownToken, a, http.StatusOK)
	call("GET", base+"/"+other.Agent.ID, "", ownToken, a, http.StatusOK)
	var removedCount, remainingPrompt int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM agent_versions WHERE agent_id = $1)
		     + (SELECT count(*) FROM agent_envs WHERE agent_id = $1)
		     + (SELECT count(*) FROM conversations WHERE agent_id = $1)
		     + (SELECT count(*) FROM messages WHERE id = $2)
		     + (SELECT count(*) FROM tool_calls WHERE id = $3),
		       (SELECT count(*) FROM prompt_versions WHERE id = $4)
	`, created.Agent.ID, messageID, toolCallID, createdPrompt.Version.ID).Scan(&removedCount, &remainingPrompt); err != nil {
		t.Fatal(err)
	}
	if removedCount != 0 || remainingPrompt != 1 {
		t.Fatalf("deletion left %d dependent rows; shared prompt count = %d", removedCount, remainingPrompt)
	}

}
