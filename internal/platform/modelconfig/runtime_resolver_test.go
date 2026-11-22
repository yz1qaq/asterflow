package modelconfig

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

func TestResolveProfile(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	cipher, err := NewCipher([]byte("runtime-resolver-test-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000004_model_profiles.up.sql", "000005_project_model_bindings.up.sql"} {
		script, err := os.ReadFile("../../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewPostgresStore(pool), cipher)

	primaryAccount, err := svc.CreateProviderAccount(ctx, CreateProviderAccountRequest{
		WorkspaceID: "space-a", Name: "primary", Kind: "openai-compatible",
		BaseURL: "https://primary.example/v1", APIKey: "primary-secret", CreatedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	backupAccount, err := svc.CreateProviderAccount(ctx, CreateProviderAccountRequest{
		WorkspaceID: "space-a", Name: "backup", Kind: "openai-compatible",
		BaseURL: "https://backup.example/v1", APIKey: "backup-secret", CreatedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := svc.CreateDeployment(ctx, CreateDeploymentRequest{
		WorkspaceID: "space-a", ProviderAccountID: primaryAccount.ID, Name: "primary",
		ModelName: "model-a", TimeoutMS: 0, MaxRetries: 2,
		InputPricePerMillion: 1.5, OutputPricePerMillion: 2.5, CreatedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := svc.CreateDeployment(ctx, CreateDeploymentRequest{
		WorkspaceID: "space-a", ProviderAccountID: backupAccount.ID, Name: "backup",
		ModelName: "model-b", TimeoutMS: 9000, MaxRetries: 0, CreatedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, version, err := svc.CreateProfile(ctx, CreateProfileRequest{
		WorkspaceID: "space-a", Name: "support", ClassificationMax: "internal",
		PrimaryDeploymentID: primary.ID, FallbackDeploymentIDs: []string{backup.ID}, CreatedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := svc.ResolveProfile(ctx, "space-a", version.ID)
	if err != nil || resolved.VersionID != version.ID || resolved.ClassificationMax != "internal" || len(resolved.Deployments) != 2 {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	first, second := resolved.Deployments[0], resolved.Deployments[1]
	if first.ID != primary.ID || first.ProviderKind != "openai-compatible" || first.ProviderID != primaryAccount.ID ||
		first.BaseURL != "https://primary.example/v1" || first.APIKey != "primary-secret" || first.Model != "model-a" ||
		first.TimeoutMS != DefaultDeploymentTimeoutMS || first.MaxRetries != 2 || first.InputPricePerMillion != 1.5 {
		t.Fatalf("primary = %+v", first)
	}
	if second.ID != backup.ID || second.APIKey != "backup-secret" || second.TimeoutMS != 9000 {
		t.Fatalf("backup = %+v", second)
	}

	if _, err := svc.ResolveProfile(ctx, "space-b", version.ID); !errors.Is(err, ErrProfileVersionNotFound) {
		t.Fatalf("cross workspace = %v", err)
	}
	if _, err := svc.ResolveProfile(ctx, "space-a", uuid.NewString()); !errors.Is(err, ErrProfileVersionNotFound) {
		t.Fatalf("unknown version = %v", err)
	}

	if _, err := pool.Exec(ctx, "UPDATE model_deployments SET status='inactive' WHERE id=$1", uuid.MustParse(backup.ID)); err != nil {
		t.Fatal(err)
	}
	resolved, err = svc.ResolveProfile(ctx, "space-a", version.ID)
	if err != nil || len(resolved.Deployments) != 1 || resolved.Deployments[0].ID != primary.ID {
		t.Fatalf("inactive deployment must be skipped: %+v, %v", resolved, err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM model_deployments WHERE id=$1", uuid.MustParse(backup.ID)); err != nil {
		t.Fatal(err)
	}
	if resolved, err = svc.ResolveProfile(ctx, "space-a", version.ID); err != nil || len(resolved.Deployments) != 1 {
		t.Fatalf("dangling fallback must be skipped: %+v, %v", resolved, err)
	}

	if _, err := pool.Exec(ctx, "UPDATE model_deployments SET status='inactive' WHERE id=$1", uuid.MustParse(primary.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveProfile(ctx, "space-a", version.ID); !errors.Is(err, llm.ErrModelUnavailable) {
		t.Fatalf("no usable deployment = %v", err)
	}

	if _, err := NewService(NewPostgresStore(pool), nil).ResolveProfile(ctx, "space-a", version.ID); err == nil {
		t.Fatal("resolution without cipher must fail when a key is present")
	}
}
