package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
	toolstore "github.com/yz1qaq/asterflow/internal/platform/tool"
)

func TestToolRegistryMigration(t *testing.T) {
	db := newMigrationDB(t)
	t.Setenv("ASTERFLOW_DATABASE_URL", db.url)
	migrations := toolMigrationFixture(t, "000012_tool_registry")
	migrate := func(action ...string) {
		t.Helper()
		if err := run(append([]string{"-path", migrations}, action...)); err != nil {
			t.Fatal(err)
		}
	}
	migrate("-up")
	q := pgstore.New(db.conn)
	tool := pgstore.CreateToolWithVersionParams{
		ToolID: uuid.New(), VersionID: uuid.New(), WorkspaceID: "space-a",
		Name: "get_order", SourceType: "rest_api", ClassificationMax: "internal",
		SchemaJson: "{}", EndpointConfig: "{}", AuthConfig: "{}", RetryPolicy: "{}",
		VersionStatus: "draft",
	}
	store := toolstore.NewPostgresStore(db.conn)
	if err := store.CreateToolWithVersion(db.ctx, &domain.Tool{
		ID: tool.ToolID.String(), WorkspaceID: tool.WorkspaceID,
		Name: tool.Name, SourceType: tool.SourceType,
	}, &domain.ToolVersion{
		ID: tool.VersionID.String(), ToolID: tool.ToolID.String(), Version: 1,
		SchemaJSON: tool.SchemaJson, EndpointConfig: tool.EndpointConfig,
		AuthConfig: tool.AuthConfig, RetryPolicy: tool.RetryPolicy, Status: tool.VersionStatus,
	}); err != nil {
		t.Fatal(err)
	}
	versions, err := q.ListToolVersions(db.ctx, tool.ToolID)
	if err != nil || len(versions) != 1 || versions[0].ID != tool.VersionID ||
		versions[0].ToolID != tool.ToolID || versions[0].Version != 1 || versions[0].Status != "draft" {
		t.Fatalf("wrong first draft: %+v, %v", versions, err)
	}

	wantUnique := func(params pgstore.CreateToolWithVersionParams) {
		t.Helper()
		var pgErr *pgconn.PgError
		err := q.CreateToolWithVersion(db.ctx, params)
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("want unique constraint failure, got %v", err)
		}
	}
	duplicate := tool
	duplicate.ToolID, duplicate.VersionID = uuid.New(), uuid.New()
	wantUnique(duplicate)

	otherSpace := duplicate
	otherSpace.WorkspaceID = "space-b"
	if err := q.CreateToolWithVersion(db.ctx, otherSpace); err != nil {
		t.Fatalf("same name in another workspace: %v", err)
	}

	orphan := tool
	orphan.ToolID, orphan.Name = uuid.New(), "should_not_exist"
	wantUnique(orphan)
	tools, err := q.ListTools(db.ctx, "space-a")
	if err != nil || len(tools) != 1 || tools[0].ID != tool.ToolID {
		t.Fatalf("version failure must leave no tool: %+v, %v", tools, err)
	}
	listed, err := store.ListTools(db.ctx, "space-a")
	if err != nil || len(listed) != 1 || listed[0].ID != tool.ToolID.String() || listed[0].Name != tool.Name {
		t.Fatalf("unexpected domain tool list: %+v, %v", listed, err)
	}
	missing, err := store.ListTools(db.ctx, "space-empty")
	if err != nil || missing == nil || len(missing) != 0 {
		t.Fatalf("want empty list: %+v, %v", missing, err)
	}

	v2ID := uuid.New()
	db.exec(t, "INSERT INTO tool_versions(id,tool_id,version,auth_config_encrypted) VALUES ($1,$2,2,$3)", v2ID, tool.ToolID, []byte{1, 2, 3})
	listedVersions, err := store.ListToolVersions(db.ctx, tool.ToolID.String())
	if err != nil || len(listedVersions) != 2 ||
		listedVersions[0].ID != v2ID.String() || listedVersions[0].ToolID != tool.ToolID.String() ||
		listedVersions[0].Version != 2 || !listedVersions[0].HasAuth || listedVersions[1].HasAuth {
		t.Fatalf("unexpected domain versions: %+v, %v", listedVersions, err)
	}
	if _, err := store.ListToolVersions(db.ctx, "not-a-uuid"); err == nil {
		t.Fatal("expected error for malformed tool ID")
	}
	service := toolstore.NewService(store)
	ownerList, err := service.ListToolVersions(db.ctx, "space-a", tool.ToolID.String())
	if err != nil || len(ownerList) != 2 || ownerList[0].ID != v2ID.String() ||
		ownerList[0].AuthConfigEncrypted != nil || ownerList[0].AuthConfig != "{}" || !ownerList[0].HasAuth {
		t.Fatalf("unexpected redacted version list: %+v, %v", ownerList, err)
	}
	if _, err := service.ListToolVersions(db.ctx, "space-b", tool.ToolID.String()); !errors.Is(err, toolstore.ErrToolNotFound) {
		t.Fatalf("cross-workspace version list: %v", err)
	}
	if _, err := service.ListToolVersions(db.ctx, "space-a", uuid.NewString()); !errors.Is(err, toolstore.ErrToolNotFound) {
		t.Fatalf("missing tool version list: %v", err)
	}
	if _, err := service.ListToolVersions(db.ctx, "space-a", "not-a-uuid"); !errors.Is(err, toolstore.ErrToolNotFound) {
		t.Fatalf("invalid tool version list: %v", err)
	}
	stored, err := store.ListToolVersions(db.ctx, tool.ToolID.String())
	if err != nil || len(stored) != 2 || len(stored[0].AuthConfigEncrypted) == 0 {
		t.Fatal("stored credential must remain after listing")
	}
	migrate("-down", "1")
	if count := db.count(t, "SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename IN ('tools', 'tool_versions')"); count != 0 {
		t.Fatalf("rollback left %d tool tables", count)
	}
	migrate("-up")
	if count := db.count(t, "SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename IN ('tools', 'tool_versions')"); count != 2 {
		t.Fatalf("reapply found %d tool tables, want 2", count)
	}
}

func TestToolTestRunsMigration(t *testing.T) {
	db := newMigrationDB(t)
	t.Setenv("ASTERFLOW_DATABASE_URL", db.url)
	migrations := toolMigrationFixture(t, "000012_tool_registry", "000013_tool_test_runs")
	migrate := func(action ...string) {
		t.Helper()
		if err := run(append([]string{"-path", migrations}, action...)); err != nil {
			t.Fatal(err)
		}
	}
	migrate("-up")
	q := pgstore.New(db.conn)
	toolID, v1, v2, otherTool, otherVersion := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	createTool := func(id, versionID uuid.UUID, name string) {
		t.Helper()
		if err := q.CreateToolWithVersion(db.ctx, pgstore.CreateToolWithVersionParams{
			ToolID: id, VersionID: versionID, WorkspaceID: "space-a", Name: name,
			SourceType: "internal_sdk", ClassificationMax: "internal",
			SchemaJson: "{}", EndpointConfig: "{}", AuthConfig: "{}", RetryPolicy: "{}", VersionStatus: "draft",
		}); err != nil {
			t.Fatal(err)
		}
	}
	createTool(toolID, v1, "get_order")
	createTool(otherTool, otherVersion, "get_inventory")
	if _, err := q.InsertToolVersion(db.ctx, pgstore.InsertToolVersionParams{
		ID: v2, ToolID: toolID, Version: 2, SchemaJson: "{}", EndpointConfig: "{}", AuthConfig: "{}", RetryPolicy: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	save := func(version uuid.UUID, status string, message pgtype.Text) pgstore.ToolTestRun {
		t.Helper()
		row, err := q.CreateToolTestRun(db.ctx, pgstore.CreateToolTestRunParams{
			ID: uuid.New(), ToolID: toolID, ToolVersionID: version, Input: `{"order_id":"fixture-1"}`,
			Output: "fixture output", Status: status, LatencyMs: 20, Error: message,
		})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	first := save(v1, "success", pgtype.Text{})
	db.exec(t, "UPDATE tool_test_runs SET created_at = created_at - interval '1 day' WHERE id = $1", first.ID)
	last := save(v1, "success", pgtype.Text{})
	save(v1, "error", pgtype.Text{String: "fixture failed", Valid: true})
	save(v2, "error", pgtype.Text{String: "fixture failed", Valid: true})

	success, err := q.GetToolLastSuccessfulTestRunForVersion(db.ctx, v1)
	if err != nil || success.ID != last.ID || success.Error.Valid || success.LatencyMs != 20 || success.CreatedAt.IsZero() {
		t.Fatalf("wrong latest successful run: %+v (%v)", success, err)
	}
	if _, err := q.GetToolLastSuccessfulTestRunForVersion(db.ctx, v2); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("v2 cannot reuse v1 success: %v", err)
	}

	const insert = "INSERT INTO tool_test_runs(id, tool_id, tool_version_id, status, latency_ms, error) VALUES ($1,$2,$3,$4,$5,$6)"
	db.wantCode(t, "23503", insert, uuid.New(), otherTool, v1, "success", 1, nil)
	db.wantCode(t, "23514", insert, uuid.New(), toolID, v2, "published", 1, nil)
	db.wantCode(t, "23514", insert, uuid.New(), toolID, v2, "success", -1, nil)
	db.wantCode(t, "23514", insert, uuid.New(), toolID, v2, "error", 1, nil)
	db.wantCode(t, "23514", insert, uuid.New(), toolID, v2, "success", 1, "unexpected error message")

	t.Run("record selected version", func(t *testing.T) {
		store := toolstore.NewPostgresStore(db.conn)
		service := toolstore.NewService(store)
		if _, err := service.RecordTestRun(db.ctx, "space-b", toolID.String(), v1.String(), "{}", "", 1, nil); !errors.Is(err, toolstore.ErrToolNotFound) {
			t.Fatalf("cross-workspace recording: %v", err)
		}
		if _, err := service.RecordTestRun(db.ctx, "space-a", toolID.String(), otherVersion.String(), "{}", "", 1, nil); err == nil {
			t.Fatal("cannot record another tool's version")
		}
		current, err := store.GetToolCurrentVersion(db.ctx, toolID.String())
		if err != nil || current.ID != v2.String() {
			t.Fatalf("fixture latest version must be v2: %+v (%v)", current, err)
		}
		run, err := service.RecordTestRun(db.ctx, "space-a", toolID.String(), v1.String(), "{}", "fixture output", 7, nil)
		if err != nil || run.ToolVersionID != v1.String() || run.Error != nil ||
			run.Status != "success" || run.LatencyMs != 7 || run.CreatedAt.IsZero() {
			t.Fatalf("run must be associated with the version executed: %+v (%v)", run, err)
		}
		latest, err := store.GetToolLastSuccessfulTestRunForVersion(db.ctx, v1.String())
		if err != nil || latest.ID != run.ID || !latest.CreatedAt.Equal(run.CreatedAt) {
			t.Fatalf("most recent successful run: %+v (%v)", latest, err)
		}
		failed, err := service.RecordTestRun(db.ctx, "space-a", toolID.String(), v2.String(), "{}", "", 4, errors.New("fixture failure"))
		if err != nil || failed.Status != "error" || failed.Error == nil || *failed.Error != "fixture failure" {
			t.Fatalf("failed run: %+v (%v)", failed, err)
		}
		if _, err := store.GetToolLastSuccessfulTestRunForVersion(db.ctx, v2.String()); !errors.Is(err, toolstore.ErrNoSuccessfulTestRun) {
			t.Fatalf("v2 cannot use another version's successful run: %v", err)
		}
	})

	db.exec(t, "DELETE FROM tool_versions WHERE id = $1", v2)
	var count int
	if err := db.conn.QueryRow(db.ctx, "SELECT count(*) FROM tool_test_runs WHERE tool_version_id = $1", v2).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleting v2 left %d test runs (%v)", count, err)
	}
	migrate("-down", "1")
	if count := db.count(t, "SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename='tool_test_runs'"); count != 0 {
		t.Fatal("rollback left tool_test_runs")
	}
	if err := db.conn.QueryRow(db.ctx, "SELECT count(*) FROM tool_versions WHERE id = $1", v1).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback removed v1 (%v)", err)
	}
	migrate("-up")
	save(v1, "success", pgtype.Text{})
}

func toolMigrationFixture(t *testing.T, batches ...string) string {
	t.Helper()
	source := filepath.Join("..", "..", "migrations")
	target := t.TempDir()
	for _, batch := range batches {
		for _, direction := range []string{"up", "down"} {
			name := batch + "." + direction + ".sql"
			contents, err := os.ReadFile(filepath.Join(source, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, name), contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return target
}
