package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
)

type fakePromptVersions struct {
	allowed map[string]bool
}

type fakeModelVersions struct {
	allowed map[string]bool
}

func (f fakePromptVersions) EnsureVersion(_ context.Context, workspaceID, versionID string) error {
	if f.allowed[workspaceID+"/"+versionID] {
		return nil
	}
	return errors.New("referenced prompt version not visible")
}

func (f fakePromptVersions) EnsurePrompt(_ context.Context, workspaceID, promptID string) error {
	if f.allowed[workspaceID+"/"+promptID] {
		return nil
	}
	return errors.New("referenced prompt not visible")
}

func (f fakeModelVersions) ValidateProfileVersion(_ context.Context, workspaceID, versionID string) error {
	if f.allowed[workspaceID+"/"+versionID] {
		return nil
	}
	return errors.New("referenced model profile version not visible")
}

func TestAgentServiceSnapshotAndPublish(t *testing.T) {
	pool := newAgentTestPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	prompts := fakePromptVersions{allowed: map[string]bool{"space-a/p1": true}}
	models := fakeModelVersions{allowed: map[string]bool{"space-a/m1": true}}
	s := NewService(store, prompts, models)

	temperature := float32(0)
	agent, version, err := s.CreateAgent(ctx, CreateAgentRequest{
		WorkspaceID: " space-a ", CreatedBy: "owner", Name: " Support ", Description: "d", Template: "customer_support",
		VersionInput: VersionInput{
			SystemPromptVersionID: "p1", ModelProfileVersionID: "m1",
			GenerationConfig: domain.GenerationConfig{Temperature: &temperature},
		},
	})
	if err != nil || agent.Name != "Support" || agent.WorkspaceID != "space-a" || version.Version != 1 {
		t.Fatalf("create = %+v %+v, %v", agent, version, err)
	}
	var snapshot domain.AgentSnapshot
	if err := json.Unmarshal([]byte(version.SnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != agent.ID || snapshot.SystemPromptVersionID != "p1" || snapshot.ModelProfileVersionID != "m1" ||
		snapshot.PromptEnv != "dev" || snapshot.GenerationConfig.Temperature == nil || *snapshot.GenerationConfig.Temperature != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	binding, err := s.GetEnvBinding(ctx, "space-a", agent.ID, "dev")
	if err != nil || binding.VersionID != version.ID {
		t.Fatalf("dev binding = %+v, %v", binding, err)
	}

	for _, tc := range []struct {
		name string
		svc  *Service
		req  CreateAgentRequest
	}{
		{"blank name", s, CreateAgentRequest{WorkspaceID: "space-a", Name: " "}},
		{"literal and pinned", s, CreateAgentRequest{WorkspaceID: "space-a", Name: "A", VersionInput: VersionInput{SystemPrompt: "x", SystemPromptVersionID: "p1"}}},
		{"unknown prompt version", s, CreateAgentRequest{WorkspaceID: "space-a", Name: "A", VersionInput: VersionInput{SystemPromptVersionID: "p9"}}},
		{"unknown model version", s, CreateAgentRequest{WorkspaceID: "space-a", Name: "A", VersionInput: VersionInput{ModelProfileVersionID: "m9"}}},
		{"no prompt resolver", NewService(store, nil, models), CreateAgentRequest{WorkspaceID: "space-a", Name: "A", VersionInput: VersionInput{SystemPromptVersionID: "p1"}}},
		{"no model resolver", NewService(store, prompts, nil), CreateAgentRequest{WorkspaceID: "space-a", Name: "A", VersionInput: VersionInput{ModelProfileVersionID: "m1"}}},
	} {
		if _, _, err := tc.svc.CreateAgent(ctx, tc.req); err == nil {
			t.Fatalf("%s: invalid create accepted", tc.name)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM agents").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed creates must not insert agents: %d, %v", count, err)
	}

	v2, err := s.AppendVersion(ctx, " space-a ", agent.ID, VersionInput{SystemPromptVersionID: "p1", PromptEnv: "staging"}, "editor")
	if err != nil || v2.Version != 2 || v2.CreatedBy != "editor" {
		t.Fatalf("append = %+v, %v", v2, err)
	}
	if err := s.Promote(ctx, "space-a", agent.ID, "dev", v2.ID); err != nil {
		t.Fatal(err)
	}
	envs, err := s.ListEnvBindings(ctx, "space-a", agent.ID)
	if err != nil || len(envs) != 1 || envs[0].Env != "dev" || envs[0].VersionID != v2.ID {
		t.Fatalf("envs = %+v, %v", envs, err)
	}
	if err := s.Promote(ctx, "space-b", agent.ID, "dev", v2.ID); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("cross-space promote = %v", err)
	}
	if _, err := s.AppendVersion(ctx, "space-b", agent.ID, VersionInput{}, "editor"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("cross-space append = %v", err)
	}
	if _, err := s.ListVersions(ctx, "space-a", uuid.NewString()); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("unknown agent versions = %v", err)
	}
	if err := s.Promote(ctx, "space-a", agent.ID, "dev", "v2"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("non-uuid version = %v", err)
	}
	versions, err := s.ListVersions(ctx, "space-a", agent.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
	list, err := s.ListAgents(ctx, "space-a")
	if err != nil || len(list) != 1 || list[0].ID != agent.ID {
		t.Fatalf("agents = %+v, %v", list, err)
	}
}
