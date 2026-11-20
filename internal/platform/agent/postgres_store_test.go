package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
)

func newAgentTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for isolated agent tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("connect to explicit agent test PostgreSQL URL")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_agent_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated agent schema: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("parse explicit agent test PostgreSQL URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create agent test pool")
	}
	t.Cleanup(pool.Close)
	for _, name := range []string{"000001_init_iam.up.sql", "000009_agents.up.sql"} {
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

func TestAgentCreateAndAppend(t *testing.T) {
	pool := newAgentTestPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)

	snapshot := `{ "system_prompt_version_id": "p1", "generation_config": {"temperature": 0} }`
	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support", Description: "d", Template: "customer_support", CreatedBy: "owner"}
	v1 := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: snapshot, CreatedBy: "owner"}
	if err := store.CreateAgentWithInitialVersion(ctx, a, v1); err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 || v1.AgentID != a.ID || v1.CreatedAt.IsZero() || a.CreatedAt.IsZero() {
		t.Fatalf("created rows = %+v %+v", a, v1)
	}
	var bound string
	if err := pool.QueryRow(ctx, "SELECT version_id::text FROM agent_envs WHERE agent_id=$1 AND env='dev'", uuid.MustParse(a.ID)).Scan(&bound); err != nil || bound != v1.ID {
		t.Fatalf("dev pointer = %q, %v", bound, err)
	}
	var stored string
	if err := pool.QueryRow(ctx, "SELECT snapshot_json FROM agent_versions WHERE id=$1", uuid.MustParse(v1.ID)).Scan(&stored); err != nil || stored != snapshot {
		t.Fatalf("snapshot fidelity = %q, %v", stored, err)
	}

	v2 := &domain.AgentVersion{ID: uuid.NewString(), AgentID: a.ID, SnapshotJSON: `{"system_prompt_version_id":"p2"}`, CreatedBy: "owner"}
	if err := store.AppendAgentVersion(ctx, "space-a", v2); err != nil {
		t.Fatal(err)
	}
	if v2.Version != 2 {
		t.Fatalf("appended version = %d", v2.Version)
	}
	if err := pool.QueryRow(ctx, "SELECT version_id::text FROM agent_envs WHERE agent_id=$1 AND env='dev'", uuid.MustParse(a.ID)).Scan(&bound); err != nil || bound != v1.ID {
		t.Fatalf("append must not move the pointer: %q, %v", bound, err)
	}
	if err := store.AppendAgentVersion(ctx, "space-b", v2); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("cross-workspace append = %v", err)
	}
	for _, bad := range []string{"", "[]", "null", "{oops"} {
		if err := store.AppendAgentVersion(ctx, "space-a", &domain.AgentVersion{ID: uuid.NewString(), AgentID: a.ID, SnapshotJSON: bad}); err == nil {
			t.Fatalf("invalid snapshot %q accepted", bad)
		}
	}
	if err := store.CreateAgentWithInitialVersion(ctx, &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: " "}, &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: "{}"}); err == nil {
		t.Fatal("blank agent name accepted")
	}
	if err := store.CreateAgentWithInitialVersion(ctx, &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Bad"}, &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: "[]"}); err == nil {
		t.Fatal("non-object snapshot accepted")
	}

	start := make(chan struct{})
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- store.AppendAgentVersion(ctx, "space-a", &domain.AgentVersion{
				ID: uuid.NewString(), AgentID: a.ID, SnapshotJSON: `{"n":1}`, CreatedBy: "owner",
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}
	rows, err := pool.Query(ctx, "SELECT version FROM agent_versions WHERE agent_id=$1 ORDER BY version", uuid.MustParse(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, n)
	}
	if len(versions) != 6 || versions[0] != 1 || versions[5] != 6 {
		t.Fatalf("concurrent versions = %v", versions)
	}

	other := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Other"}
	otherV1 := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: "{}"}
	if err := store.CreateAgentWithInitialVersion(ctx, other, otherV1); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO agent_envs(agent_id,env,version_id) VALUES($1,'prod',$2)", uuid.MustParse(other.ID), uuid.MustParse(v1.ID)); err == nil {
		t.Fatal("env must not bind a version owned by another agent")
	}
}

func TestAgentReadAndPublish(t *testing.T) {
	pool := newAgentTestPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)

	a := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support", Template: "general", CreatedBy: "owner"}
	v1 := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: `{"p":"1"}`, CreatedBy: "owner"}
	if err := store.CreateAgentWithInitialVersion(ctx, a, v1); err != nil {
		t.Fatal(err)
	}
	v2 := &domain.AgentVersion{ID: uuid.NewString(), AgentID: a.ID, SnapshotJSON: `{"p":"2"}`, CreatedBy: "owner"}
	if err := store.AppendAgentVersion(ctx, "space-a", v2); err != nil {
		t.Fatal(err)
	}
	other := &domain.Agent{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Billing"}
	otherV1 := &domain.AgentVersion{ID: uuid.NewString(), SnapshotJSON: "{}"}
	if err := store.CreateAgentWithInitialVersion(ctx, other, otherV1); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetAgent(ctx, "space-a", a.ID)
	if err != nil || got.Name != "Support" || got.Template != "general" || got.Description != "" {
		t.Fatalf("get agent = %+v, %v", got, err)
	}
	agents, err := store.ListAgents(ctx, "space-a")
	if err != nil || len(agents) != 2 || agents[0].Name != "Billing" || agents[1].Name != "Support" {
		t.Fatalf("list agents = %+v, %v", agents, err)
	}
	for _, tc := range []struct{ space, id string }{{"space-b", a.ID}, {"space-a", uuid.NewString()}} {
		if _, err := store.GetAgent(ctx, tc.space, tc.id); !errors.Is(err, ErrAgentNotFound) {
			t.Fatalf("hidden agent %+v = %v", tc, err)
		}
	}
	if _, err := store.GetAgent(ctx, "space-a", "bad"); err == nil {
		t.Fatal("invalid agent id accepted")
	}

	versions, err := store.ListAgentVersions(ctx, "space-a", a.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("list versions = %+v, %v", versions, err)
	}
	if got, err := store.GetAgentVersion(ctx, "space-a", v2.ID); err != nil || got.Version != 2 || got.SnapshotJSON != `{"p":"2"}` {
		t.Fatalf("get version = %+v, %v", got, err)
	}
	if _, err := store.GetAgentVersion(ctx, "space-b", v2.ID); !errors.Is(err, ErrAgentVersionNotFound) {
		t.Fatalf("cross-space version = %v", err)
	}

	if binding, err := store.GetAgentEnv(ctx, "space-a", a.ID, "dev"); err != nil || binding.VersionID != v1.ID {
		t.Fatalf("dev binding = %+v, %v", binding, err)
	}
	if _, err := store.GetAgentEnv(ctx, "space-a", a.ID, "prod"); !errors.Is(err, ErrAgentEnvNotBound) {
		t.Fatalf("unbound env = %v", err)
	}
	if _, err := store.GetAgentEnv(ctx, "space-b", a.ID, "dev"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("cross-space env = %v", err)
	}

	if err := store.SetAgentEnv(ctx, "space-a", a.ID, "staging", v1.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgentEnv(ctx, "space-a", a.ID, "dev", v2.ID); err != nil {
		t.Fatal(err)
	}
	if binding, err := store.GetAgentEnv(ctx, "space-a", a.ID, "dev"); err != nil || binding.VersionID != v2.ID || binding.UpdatedAt.IsZero() {
		t.Fatalf("moved dev binding = %+v, %v", binding, err)
	}
	if binding, err := store.GetAgentEnv(ctx, "space-a", a.ID, "staging"); err != nil || binding.VersionID != v1.ID {
		t.Fatalf("staging binding = %+v, %v", binding, err)
	}
	if err := store.SetAgentEnv(ctx, "space-a", a.ID, "prod", otherV1.ID); !errors.Is(err, ErrAgentVersionNotFound) {
		t.Fatalf("foreign agent version = %v", err)
	}
	if err := store.SetAgentEnv(ctx, "space-b", a.ID, "prod", v2.ID); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("cross-space publish = %v", err)
	}
	if err := store.SetAgentEnv(ctx, "space-a", a.ID, " ", v2.ID); err == nil {
		t.Fatal("empty env accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE agent_versions SET snapshot_json='not-json' WHERE id=$1", uuid.MustParse(v2.ID)); err == nil {
		t.Fatal("database must reject invalid snapshot json")
	}
}
