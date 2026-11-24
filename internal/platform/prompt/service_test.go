package prompt

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
)

func TestPromptCreateAndRenderService(t *testing.T) {
	pool := newPromptTestPool(t)
	ctx := context.Background()
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')")
	modelVersion := func(space string) string {
		t.Helper()
		a, d, p, v := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		exec("INSERT INTO providers(id,workspace_id,name,kind,base_url) VALUES($1,$2,'provider','example','https://example.invalid')", a, space)
		exec("INSERT INTO model_deployments(id,workspace_id,provider_account_id,name,model_name) VALUES($1,$2,$3,'main','example')", d, space, a)
		exec("INSERT INTO model_profiles(id,workspace_id,name) VALUES($1,$2,'Support')", p, space)
		exec("INSERT INTO model_profile_versions(id,profile_id,version,primary_deployment_id) VALUES($1,$2,1,$3)", v, p, d)
		return v.String()
	}
	modelA, modelB := modelVersion("space-a"), modelVersion("space-b")
	store := NewPostgresStore(pool)
	s := NewService(store, modelconfig.NewService(modelconfig.NewPostgresStore(pool), nil))

	temp := float32(0)
	req := CreatePromptRequest{WorkspaceID: " space-a ", CreatedBy: "actor", Name: " Order check ", Category: " system ",
		VersionInput: VersionInput{Template: "Check {{.order_id}}", VariablesSchema: `{ "required": ["order_id"] }`,
			ModelProfileVersionID: " " + modelA + " ", GenerationConfig: domain.GenerationConfig{Temperature: &temp}}}
	p, v1, err := s.CreatePrompt(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Order check" || p.WorkspaceID != "space-a" || p.Category != "system" || p.CreatedBy != "actor" ||
		v1.Version != 1 || v1.PromptID != p.ID || v1.ModelProfileVersionID != modelA ||
		v1.VariablesSchema != req.VariablesSchema || v1.GenerationConfig.Temperature == nil ||
		*v1.GenerationConfig.Temperature != 0 || v1.TokenEstimate < 1 ||
		v1.Hash != fmt.Sprintf("%x", sha256.Sum256([]byte(req.Template))) {
		t.Fatal("compiled metadata and model settings must be persisted unchanged")
	}
	for _, invalid := range []VersionInput{
		{Template: "Check {{.order_id", VariablesSchema: "{}"},
		{Template: "Check {{.order_id}}", VariablesSchema: `{"required":null}`},
	} {
		bad := req
		bad.VersionInput = invalid
		if _, _, err := s.CreatePrompt(ctx, bad); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid draft must not reach the database: %v", err)
		}
		if _, err := s.CreateVersion(ctx, p.ID, CreateVersionRequest{WorkspaceID: "space-a", VersionInput: invalid}); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid append should be rejected: %v", err)
		}
	}
	for _, id := range []string{modelB, uuid.NewString()} {
		bad := req
		bad.ModelProfileVersionID = id
		if _, _, err := s.CreatePrompt(ctx, bad); !errors.Is(err, modelconfig.ErrProfileVersionNotFound) {
			t.Fatalf("foreign or missing model profile version must not be referenced: %v", err)
		}
	}
	bad := req
	bad.ModelProfileVersionID = "invalid"
	if _, _, err := s.CreatePrompt(ctx, bad); !errors.Is(err, modelconfig.ErrInvalidArgument) {
		t.Fatalf("malformed model profile UUID must be invalid: %v", err)
	}
	if prompts, err := store.ListPrompts(ctx, "space-a"); err != nil || len(prompts) != 1 {
		t.Fatalf("invalid create left a partial prompt: %v", err)
	}

	v2, err := s.CreateVersion(ctx, p.ID, CreateVersionRequest{WorkspaceID: "space-a", CreatedBy: "editor",
		VersionInput: VersionInput{Template: "Track {{.order_id}}", VariablesSchema: " "}})
	if err != nil || v2.Version != 2 || v2.VariablesSchema != "{}" || v2.CreatedBy != "editor" {
		t.Fatalf("append should allocate v2 without skipped numbers: %v", err)
	}
	var dev string
	if err := pool.QueryRow(ctx, "SELECT version_id::text FROM prompt_envs WHERE prompt_id=$1 AND env='dev'", uuid.MustParse(p.ID)).Scan(&dev); err != nil || dev != v1.ID {
		t.Fatalf("append moved the dev pointer: %v", err)
	}
	for id, want := range map[string]string{v1.ID: "Check ORD-1001", v2.ID: "Track ORD-1001"} {
		got, err := s.RenderByVersion(ctx, "space-a", p.ID, id, map[string]any{"order_id": "ORD-1001"})
		if err != nil || got != want {
			t.Fatalf("preview returned %q, want %q: %v", got, want, err)
		}
	}
	if got, err := s.RenderByVersion(ctx, "space-a", p.ID, v1.ID, nil); got != "" || !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), "order_id") {
		t.Fatalf("missing required variable should be a readable client error: %v", err)
	}
	for _, pair := range [][2]string{{"space-b", p.ID}, {"space-a", uuid.NewString()}} {
		if _, err := s.RenderByVersion(ctx, pair[0], pair[1], v1.ID, nil); !errors.Is(err, ErrPromptVersionNotFound) {
			t.Fatalf("mismatched preview scope should not expose template: %v", err)
		}
	}
}

type promptSelectionStore struct {
	Store
	experiment *domain.PromptExperiment
}

func (s promptSelectionStore) GetActiveExperiment(context.Context, string, string, string) (*domain.PromptExperiment, error) {
	return s.experiment, nil
}

func (s promptSelectionStore) GetEnvBinding(context.Context, string, string, string) (*EnvBinding, error) {
	return &EnvBinding{VersionID: "baseline"}, nil
}

func TestResolveSelectionKeepsRolloutSource(t *testing.T) {
	promptID := uuid.NewString()
	for _, tt := range []struct {
		name string
		exp  *domain.PromptExperiment
		want domain.PromptSelection
	}{
		{name: "plain environment", want: domain.PromptSelection{VersionID: "baseline"}},
		{
			name: "candidate",
			exp: &domain.PromptExperiment{
				ID: "rollout", CandidateVersionID: "candidate", TrafficPercent: 100,
				Variants: []domain.ExperimentVariant{{VersionID: "candidate", Traffic: 100}},
			},
			want: domain.PromptSelection{VersionID: "candidate", ExperimentID: "rollout", ExperimentVariant: "candidate"},
		},
		{
			name: "baseline",
			exp: &domain.PromptExperiment{
				ID: "rollout", CandidateVersionID: "candidate", TrafficPercent: 1,
				Variants: []domain.ExperimentVariant{{VersionID: "candidate", Traffic: 1}},
			},
			want: domain.PromptSelection{VersionID: "baseline", ExperimentID: "rollout", ExperimentVariant: "baseline"},
		},
		{
			name: "multiple variants",
			exp: &domain.PromptExperiment{
				ID: "experiment", Variants: []domain.ExperimentVariant{
					{VersionID: "a", Traffic: 1}, {VersionID: "b", Traffic: 99},
				},
			},
			want: domain.PromptSelection{VersionID: "b", ExperimentID: "experiment", ExperimentVariant: "variant"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(promptSelectionStore{experiment: tt.exp}, nil)
			ctx := context.Background()
			got, err := svc.ResolveSelection(ctx, "space-a", promptID, "dev", "user-1")
			if err != nil || got != tt.want {
				t.Fatalf("selection = %+v, %v, want %+v", got, err, tt.want)
			}
			if versionID, err := svc.ResolveVersion(ctx, "space-a", promptID, "dev", "user-1"); err != nil || versionID != got.VersionID {
				t.Fatalf("version resolver = %q, %v", versionID, err)
			}
		})
	}
}
