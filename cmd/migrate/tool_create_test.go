package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
)

func TestRegisterTool(t *testing.T) {
	db := newMigrationDB(t)
	t.Setenv("ASTERFLOW_DATABASE_URL", db.url)
	if err := run([]string{"-path", filepath.Join("..", "..", "migrations"), "-up"}); err != nil {
		t.Fatal(err)
	}
	store := tool.NewPostgresStore(db.conn)
	service := tool.NewService(store)
	cipher, err := modelconfig.NewCipher([]byte("registration-fixture-key"))
	if err != nil {
		t.Fatal(err)
	}
	policy := tool.NewEndpointPolicy([]string{"crossborder-sim"}, false)
	service.ConfigureSecurity(cipher, policy)

	original := tool.CreateToolRequest{
		WorkspaceID: "space-a", CreatedBy: "actor-a", Name: "  read_order  ", SourceType: "rest_api",
		SchemaJSON:     `{"type":"object"}`,
		EndpointConfig: `{"url":"http://crossborder-sim:8091/tools/get_order"}`,
		AuthConfig:     `{"bearer_token":"fixture-only"}`,
	}
	item, err := service.CreateTool(db.ctx, original)
	if err != nil || item.Name != "read_order" || item.WorkspaceID != "space-a" {
		t.Fatalf("create tool: %+v, %v", item, err)
	}
	versions, err := store.ListToolVersions(db.ctx, item.ID)
	if err != nil || len(versions) != 1 || versions[0].ID == item.ID ||
		versions[0].ToolID != item.ID || versions[0].Version != 1 || versions[0].Status != "draft" ||
		versions[0].AuthConfig != "{}" || !versions[0].HasAuth {
		t.Fatal("want owned v1 draft with encrypted credential")
	}
	plaintext, err := cipher.Decrypt(versions[0].AuthConfigEncrypted)
	if err != nil || plaintext != original.AuthConfig {
		t.Fatal("stored credential does not decrypt to the original fixture")
	}

	t.Run("load current and pinned configuration", func(t *testing.T) {
		first, err := service.GetToolConfigForTest(db.ctx, item.WorkspaceID, item.ID)
		if err != nil || first.Tool.ID != item.ID || first.VersionID != versions[0].ID ||
			first.Config.AuthConfig["bearer_token"] != "fixture-only" {
			t.Fatalf("load the original configuration: %v", err)
		}

		const endpoint = "http://crossborder-sim:8091/tools/get_order_v2"
		second, err := service.CreateToolVersion(db.ctx, item.WorkspaceID, item.ID, tool.CreateToolVersionRequest{
			EndpointConfig: `{"url":"` + endpoint + `"}`,
			AuthConfig:     `{"bearer_token":"fixture-second-token"}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		latest, err := service.GetToolConfigForTest(db.ctx, item.WorkspaceID, item.ID)
		if err != nil || latest.VersionID != second.ID || latest.Config.EndpointConfig["url"] != endpoint ||
			latest.Config.AuthConfig["bearer_token"] != "fixture-second-token" {
			t.Fatalf("load the current configuration: %v", err)
		}
		pinned, err := service.GetToolConfigByVersion(db.ctx, item.WorkspaceID, first.VersionID)
		if err != nil || pinned.VersionID != first.VersionID ||
			pinned.Config.EndpointConfig["url"] != first.Config.EndpointConfig["url"] ||
			pinned.Config.AuthConfig["bearer_token"] != "fixture-only" {
			t.Fatalf("loading a pinned version must not fall back to current: %v", err)
		}
		payload, err := json.Marshal(latest)
		if err != nil || strings.Contains(string(payload), "fixture-second-token") {
			t.Fatalf("execution config must not be serialized: %v", err)
		}
		if _, err := service.GetToolConfigForTest(db.ctx, "space-b", item.ID); !errors.Is(err, tool.ErrToolNotFound) {
			t.Fatalf("cross-workspace tool load: %v", err)
		}
		if _, err := service.GetToolConfigByVersion(db.ctx, "space-b", first.VersionID); !errors.Is(err, tool.ErrToolNotFound) {
			t.Fatalf("cross-workspace version load: %v", err)
		}
		if _, err := service.GetToolConfigByVersion(db.ctx, item.WorkspaceID, "not-a-uuid"); !errors.Is(err, tool.ErrToolNotFound) {
			t.Fatalf("malformed version ID: %v", err)
		}
	})

	rejected := original
	rejected.Name, rejected.EndpointConfig = "without_endpoint", "{}"
	if _, err := service.CreateTool(db.ctx, rejected); !errors.Is(err, tool.ErrInvalidArgument) {
		t.Fatalf("want endpoint validation error, got %v", err)
	}
	service.ConfigureSecurity(nil, policy)
	rejected = original
	rejected.Name = "without_cipher"
	if _, err := service.CreateTool(db.ctx, rejected); err == nil {
		t.Fatal("expected encryption dependency error")
	}
	service.ConfigureSecurity(cipher, nil)
	if _, err := service.CreateTool(db.ctx, rejected); err == nil {
		t.Fatal("expected missing endpoint policy to reject registration")
	}
	if count := db.count(t, "SELECT count(*) FROM tools"); count != 1 {
		t.Fatalf("rejected registrations changed SQL count to %d", count)
	}
}

func TestAppendToolVersion(t *testing.T) {
	db := newMigrationDB(t)
	t.Setenv("ASTERFLOW_DATABASE_URL", db.url)
	if err := run([]string{"-path", filepath.Join("..", "..", "migrations"), "-up"}); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(db.ctx, db.url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := tool.NewPostgresStore(pool)
	item := &domain.Tool{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "get_order", SourceType: "internal_sdk"}
	initial := &domain.ToolVersion{ID: uuid.NewString(), ToolID: item.ID, Version: 1, Status: "draft", SchemaJSON: "{}", EndpointConfig: "{}", AuthConfig: "{}", RetryPolicy: "{}"}
	if err := store.CreateToolWithVersion(db.ctx, item, initial); err != nil {
		t.Fatal(err)
	}
	draft := func() *domain.ToolVersion {
		return &domain.ToolVersion{ID: uuid.NewString(), ToolID: item.ID, SchemaJSON: "{}", EndpointConfig: `{"operation":"get_order"}`, AuthConfig: "{}", RetryPolicy: "{}"}
	}
	if _, err := store.GetToolCurrentVersion(db.ctx, "invalid"); !errors.Is(err, tool.ErrToolNotFound) {
		t.Fatalf("invalid parent: %v", err)
	}
	if err := store.AppendToolVersion(db.ctx, "space-b", 0, draft()); !errors.Is(err, tool.ErrToolNotFound) {
		t.Fatalf("cross-tenant append: %v", err)
	}
	duplicate := draft()
	duplicate.ID = initial.ID
	if err := store.AppendToolVersion(db.ctx, item.WorkspaceID, 0, duplicate); err == nil || duplicate.Version != 0 {
		t.Fatalf("failed insert should leave draft untouched: %+v, %v", duplicate, err)
	}
	const count = 8
	type result struct {
		version *domain.ToolVersion
		err     error
	}
	results := make(chan result, count)
	start := make(chan struct{})
	for range count {
		go func() {
			v := draft()
			<-start
			results <- result{v, store.AppendToolVersion(db.ctx, item.WorkspaceID, 0, v)}
		}()
	}
	close(start)
	byNumber := make(map[int]bool, count)
	for range count {
		r := <-results
		if r.err != nil {
			t.Errorf("concurrent append: %v", r.err)
			continue
		}
		v := r.version
		if v.Status != "draft" || v.CreatedAt.IsZero() || byNumber[v.Version] || v.ToolID != item.ID {
			t.Errorf("unexpected version: %+v", v)
		}
		byNumber[v.Version] = true
	}
	for version := 2; version <= count+1; version++ {
		if !byNumber[version] {
			t.Errorf("missing draft v%d", version)
		}
	}
	current, err := store.GetToolCurrentVersion(db.ctx, item.ID)
	if err != nil || current.Version != count+1 || current.EndpointConfig != draft().EndpointConfig {
		t.Fatalf("current version: %+v, %v", current, err)
	}
	versions, err := store.ListToolVersions(db.ctx, item.ID)
	if err != nil || len(versions) != count+1 {
		t.Fatalf("list after append: %+v, %v", versions, err)
	}
	if oldest := versions[len(versions)-1]; oldest.ID != initial.ID || oldest.EndpointConfig != initial.EndpointConfig {
		t.Fatalf("initial draft was changed: %+v", oldest)
	}
}
