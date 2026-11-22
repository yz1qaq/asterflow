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

func TestConversationHTTP(t *testing.T) {
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

	identities := iam.NewService(iam.NewPostgresStore(pool), []byte("conversation-http-fixture-key-32-bytes"))
	createUser := func(email, name string) *domain.User {
		t.Helper()
		user, err := identities.CreateUser(ctx, email, "fixture-password123", name)
		if err != nil {
			t.Fatal(err)
		}
		return user
	}
	owner := createUser("owner@conv.test", "Owner")
	member := createUser("member@conv.test", "Member")
	other := createUser("other@conv.test", "Other")
	viewer := createUser("viewer@conv.test", "Viewer")
	makeSpace := func(name string) string {
		t.Helper()
		ws, err := identities.CreateWorkspaceForUser(ctx, owner.ID, name, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return ws.ID
	}
	a, b := makeSpace("A"), makeSpace("B")
	for _, role := range []struct {
		user *domain.User
		role string
	}{{member, iam.WorkspaceRoleMember}, {other, iam.WorkspaceRoleMember}, {viewer, iam.WorkspaceRoleViewer}} {
		if _, err := identities.UpsertWorkspaceMember(ctx, owner.ID, a, role.user.ID, role.role); err != nil {
			t.Fatal(err)
		}
	}
	tokenFor := func(email string) string {
		t.Helper()
		res, err := identities.Login(ctx, iam.LoginRequest{Email: email, Password: "fixture-password123"})
		if err != nil {
			t.Fatal(err)
		}
		return res.Token
	}
	ownerToken, memberToken, otherToken, viewerToken := tokenFor(owner.Email), tokenFor(member.Email), tokenFor(other.Email), tokenFor(viewer.Email)

	models := modelconfig.NewService(modelconfig.NewPostgresStore(pool), nil)
	prompts := prompt.NewService(prompt.NewPostgresStore(pool), models)
	agents := agent.NewService(agent.NewPostgresStore(pool), prompts, models)
	conversations := conversation.NewService(conversation.NewPostgresStore(pool), agents, nil, nil)
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

	var createdAgent struct {
		Agent   domain.Agent        `json:"agent"`
		Version domain.AgentVersion `json:"version"`
	}
	if err := json.Unmarshal(call("POST", "/api/v1/agents", marshal(map[string]any{
		"name": "Support", "system_prompt": "hi",
	}), ownerToken, a, http.StatusCreated), &createdAgent); err != nil {
		t.Fatal(err)
	}
	agentID, versionID := createdAgent.Agent.ID, createdAgent.Version.ID

	base := "/api/v1/conversations"
	call("POST", base, marshal(map[string]any{"agent_id": agentID, "agent_version_id": versionID}), viewerToken, a, http.StatusForbidden)
	call("POST", base, marshal(map[string]any{"agent_id": agentID, "agent_version_id": uuid.NewString()}), memberToken, a, http.StatusNotFound)
	call("POST", base, marshal(map[string]any{"agent_id": agentID, "agent_version_id": versionID}), memberToken, b, http.StatusForbidden)

	var created domain.Conversation
	if err := json.Unmarshal(call("POST", base, marshal(map[string]any{
		"agent_id": agentID, "agent_version_id": versionID, "classification": "internal",
	}), memberToken, a, http.StatusCreated), &created); err != nil {
		t.Fatal(err)
	}
	if created.UserID != member.ID || created.AgentVersionID != versionID || created.Status != "active" {
		t.Fatalf("created = %+v", created)
	}

	listOf := func(token string, want int) []domain.Conversation {
		t.Helper()
		var items []domain.Conversation
		if err := json.Unmarshal(call("GET", base, "", token, a, want), &items); err != nil {
			t.Fatal(err)
		}
		return items
	}
	if items := listOf(memberToken, http.StatusOK); len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("member list = %+v", items)
	}
	if items := listOf(otherToken, http.StatusOK); len(items) != 0 {
		t.Fatalf("other member must not see the conversation: %+v", items)
	}
	if items := listOf(ownerToken, http.StatusOK); len(items) != 1 {
		t.Fatalf("owner list = %+v", items)
	}

	var detail struct {
		Conversation domain.Conversation `json:"conversation"`
		Messages     []domain.Message    `json:"messages"`
	}
	if err := json.Unmarshal(call("GET", base+"/"+created.ID, "", memberToken, a, http.StatusOK), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Conversation.ID != created.ID || detail.Messages == nil {
		t.Fatalf("detail = %+v", detail)
	}
	call("GET", base+"/"+created.ID, "", otherToken, a, http.StatusNotFound)
	call("GET", base+"/"+created.ID, "", ownerToken, a, http.StatusOK)
	call("GET", base+"/"+created.ID, "", ownerToken, b, http.StatusNotFound)
	call("GET", base+"/invalid", "", memberToken, a, http.StatusBadRequest)
}
