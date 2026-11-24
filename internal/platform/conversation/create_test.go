package conversation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
)

// fakePromptChecker lets the agent service accept prompt-id references in snapshots.
type fakePromptChecker struct{}

func (fakePromptChecker) EnsureVersion(context.Context, string, string) error { return nil }
func (fakePromptChecker) EnsurePrompt(context.Context, string, string) error  { return nil }

// fakePromptResolver records which prompt/env/user it resolved and returns a fixed version.
type fakePromptResolver struct {
	resolved string
	calls    []string
}

func (f *fakePromptResolver) ResolveSelection(_ context.Context, _ string, promptID, env, userID string) (domain.PromptSelection, error) {
	f.calls = append(f.calls, promptID+"/"+env+"/"+userID)
	return domain.PromptSelection{VersionID: f.resolved, ExperimentID: "experiment-a", ExperimentVariant: "candidate"}, nil
}

func TestConversationCreateResolvesPromptIDs(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()

	agentStore := agent.NewPostgresStore(pool)
	agentSvc := agent.NewService(agentStore, fakePromptChecker{}, nil)
	created, version, err := agentSvc.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "space-a", CreatedBy: "owner", Name: "Support",
		VersionInput: agent.VersionInput{
			SystemPromptID: "prompt-a", UserPromptID: "prompt-b", PromptEnv: "dev",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &fakePromptResolver{resolved: "resolved-version-id"}
	store := NewPostgresStore(pool)
	svc := NewService(store, agentSvc, resolver, nil)

	conv, err := svc.Create(ctx, CreateConversationRequest{
		WorkspaceID: "space-a", UserID: "user-1",
		AgentID: created.ID, AgentVersionID: version.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conv.AgentVersionID != version.ID || conv.RuntimeConfigJSON == "" {
		t.Fatalf("conversation = %+v", conv)
	}

	var runtime domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(conv.RuntimeConfigJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Environment != "dev" || runtime.SystemPromptVersionID != "resolved-version-id" ||
		runtime.UserPromptVersionID != "resolved-version-id" || runtime.UserPromptExperiment != "experiment-a" || runtime.UserPromptVariant != "candidate" {
		t.Fatalf("runtime config = %+v", runtime)
	}
	if len(resolver.calls) != 2 || resolver.calls[0] != "prompt-a/dev/user-1" || resolver.calls[1] != "prompt-b/dev/user-1" {
		t.Fatalf("resolver calls = %v", resolver.calls)
	}
}

func TestConversationCreatePrefersPinnedVersion(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()

	agentStore := agent.NewPostgresStore(pool)
	agentSvc := agent.NewService(agentStore, fakePromptChecker{}, nil)
	created, version, err := agentSvc.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "space-a", CreatedBy: "owner", Name: "Support",
		VersionInput: agent.VersionInput{
			SystemPromptVersionID: "pinned-version", PromptEnv: "dev",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &fakePromptResolver{resolved: "resolved-version-id"}
	store := NewPostgresStore(pool)
	svc := NewService(store, agentSvc, resolver, nil)

	conv, err := svc.Create(ctx, CreateConversationRequest{
		WorkspaceID: "space-a", UserID: "user-1", AgentID: created.ID, AgentVersionID: version.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var runtime domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(conv.RuntimeConfigJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.SystemPromptVersionID != "pinned-version" {
		t.Fatalf("pinned version must be preserved: %+v", runtime)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("pinned version must not hit the resolver: %v", resolver.calls)
	}
}

func TestConversationCreateRequiresResolver(t *testing.T) {
	pool := newConversationTestPool(t)
	ctx := context.Background()

	agentStore := agent.NewPostgresStore(pool)
	agentSvc := agent.NewService(agentStore, fakePromptChecker{}, nil)
	created, version, err := agentSvc.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "space-a", CreatedBy: "owner", Name: "Support",
		VersionInput: agent.VersionInput{SystemPromptID: "prompt-a", PromptEnv: "dev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	svc := NewService(store, agentSvc, nil, nil)

	if _, err := svc.Create(ctx, CreateConversationRequest{
		WorkspaceID: "space-a", UserID: "user-1", AgentID: created.ID, AgentVersionID: version.ID,
	}); err == nil {
		t.Fatal("prompt-id snapshot without resolver must fail")
	}
	_ = uuid.NewString
}
