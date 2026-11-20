package prompt

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
)

func newPromptTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for isolated PostgreSQL prompt tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("connect to explicit prompt test PostgreSQL URL")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_prompt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated prompt schema: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("parse explicit prompt test PostgreSQL URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create prompt test pool")
	}
	t.Cleanup(pool.Close)
	for _, migration := range []string{
		"000001_init_iam.up.sql", "000002_workspace_rbac.up.sql",
		"000003_model_accounts_deployments.up.sql", "000004_model_profiles.up.sql",
		"000005_project_model_bindings.up.sql",
	} {
		applyPromptMigration(t, pool, migration)
	}
	return pool
}

func applyPromptMigration(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	text, err := os.ReadFile("../../../migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, string(text)); err != nil {
		t.Fatalf("apply %s in isolated prompt schema: %v", name, err)
	}
}

func TestPromptCoreMigration(t *testing.T) {
	pool := newPromptTestPool(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(code, query string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, query, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Fatalf("want SQLSTATE %s, got %v", code, err)
		}
	}

	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	exec("INSERT INTO workspaces(id,name) VALUES ('space-a','A')")
	p, other, v1, v2, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	reject("23503", "INSERT INTO prompts(id,workspace_id,name) VALUES($1,'missing','Support')", uuid.New())
	exec("INSERT INTO prompts(id,workspace_id,name) VALUES($1,'space-a','Support'),($2,'space-a','Support')", p, other)
	const vars = `{  "required": ["order_id"] }`
	exec("INSERT INTO prompt_versions(id,prompt_id,version,template,variables_schema) VALUES($1,$2,1,'old',$3),($4,$5,1,'other','{}')", v1, p, vars, foreign, other)
	exec("INSERT INTO prompt_versions(id,prompt_id,version,template,generation_config) VALUES($1,$2,2,'new','{\"temperature\":0}')", v2, p)
	reject("23505", "INSERT INTO prompt_versions(id,prompt_id,version,template) VALUES ($1,$2,1,'duplicate')", uuid.New(), p)
	reject("23514", "INSERT INTO prompt_versions(id,prompt_id,version,template) VALUES ($1,$2,0,'bad')", uuid.New(), p)
	reject("23514", "INSERT INTO prompt_versions(id,prompt_id,version,template,generation_config) VALUES ($1,$2,3,'bad','[]')", uuid.New(), p)
	reject("23503", "INSERT INTO prompt_envs(prompt_id,env,version_id) VALUES($1,'dev',$2)", p, foreign)
	exec("INSERT INTO prompt_envs(prompt_id,env,version_id) VALUES($1,'dev',$2),($1,'staging',$3)", p, v1, v2)
	var storedVars string
	var defaults bool
	err := pool.QueryRow(ctx, "SELECT variables_schema, generation_config='{}'::jsonb AND model_profile_version_id IS NULL FROM prompt_versions WHERE id=$1", v1).Scan(&storedVars, &defaults)
	if err != nil || storedVars != vars || !defaults {
		t.Fatalf("version raw schema/defaults mismatch: %v", err)
	}
	var stillDev uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT version_id FROM prompt_envs WHERE prompt_id=$1 AND env='dev'", p).Scan(&stillDev); err != nil || stillDev != v1 {
		t.Fatalf("staging write changed dev: %v", err)
	}
	exec("DELETE FROM prompts WHERE id=$1", p)
	for _, table := range []string{"prompt_envs", "prompt_versions"} {
		var remaining int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE prompt_id=$1", p).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatalf("related rows not removed: %v", err)
		}
	}
	applyPromptMigration(t, pool, "000006_prompt_versions.down.sql")
	var clean bool
	err = pool.QueryRow(ctx, "SELECT to_regclass('prompts') IS NULL AND to_regclass('prompt_envs') IS NULL AND to_regclass('prompt_versions') IS NULL AND to_regclass('project_model_bindings') IS NOT NULL").Scan(&clean)
	if err != nil || !clean {
		t.Fatalf("rollback affected the wrong tables: %v", err)
	}
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
}

func TestCreatePromptWithInitialVersion(t *testing.T) {
	pool := newPromptTestPool(t)
	ctx := context.Background()
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO workspaces(id,name) VALUES ('space-a','A')")
	accountID, deploymentID, profileID, modelVersionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec("INSERT INTO providers(id,workspace_id,name,kind,base_url) VALUES($1,'space-a','model-test','example','https://example.invalid')", accountID)
	exec("INSERT INTO model_deployments(id,workspace_id,provider_account_id,name,model_name) VALUES($1,'space-a',$2,'main','example')", deploymentID, accountID)
	exec("INSERT INTO model_profiles(id,workspace_id,name) VALUES($1,'space-a','Support')", profileID)
	exec("INSERT INTO model_profile_versions(id,profile_id,version,primary_deployment_id) VALUES($1,$2,1,$3)", modelVersionID, profileID, deploymentID)

	store := NewPostgresStore(pool)
	makeRecords := func() (Prompt, PromptVersion) {
		p := Prompt{ID: strings.ReplaceAll(uuid.NewString(), "-", ""), WorkspaceID: "space-a", Name: "order check", CreatedBy: "actor"}
		temp := float32(0)
		v := PromptVersion{ID: uuid.NewString(), PromptID: p.ID, Version: 1, Template: "Check {{.order_id}}",
			VariablesSchema: `{  "required": ["order_id"] }`, ModelProfileVersionID: modelVersionID.String(),
			GenerationConfig: domain.GenerationConfig{Temperature: &temp}, TokenEstimate: 8, CreatedBy: "actor"}
		return p, v
	}

	p, v := makeRecords()
	rawVars := v.VariablesSchema
	if err := store.CreatePromptWithInitialVersion(ctx, &p, &v); err != nil {
		t.Fatal(err)
	}
	if p.ID != uuid.MustParse(p.ID).String() || v.PromptID != p.ID ||
		p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || v.CreatedAt.IsZero() ||
		v.VariablesSchema != rawVars || v.ModelProfileVersionID != modelVersionID.String() ||
		v.GenerationConfig.Temperature == nil || *v.GenerationConfig.Temperature != 0 {
		t.Fatal("persisted prompt and initial version missing canonical fields")
	}
	var devID uuid.UUID
	var storedTemp string
	err := pool.QueryRow(ctx, `SELECT e.version_id, v.generation_config->>'temperature'
		FROM prompt_envs e JOIN prompt_versions v ON v.id=e.version_id
		WHERE e.prompt_id=$1 AND e.env='dev'`, uuid.MustParse(p.ID)).Scan(&devID, &storedTemp)
	if err != nil || devID.String() != v.ID || storedTemp != "0" {
		t.Fatalf("initial dev pointer must reference v1 with explicit zero temperature: %v", err)
	}
	noModelP, noModelV := makeRecords()
	noModelV.ModelProfileVersionID = ""
	noModelV.GenerationConfig = domain.GenerationConfig{}
	if err := store.CreatePromptWithInitialVersion(ctx, &noModelP, &noModelV); err != nil {
		t.Fatal(err)
	}
	if noModelV.ModelProfileVersionID != "" || noModelV.GenerationConfig.Temperature != nil {
		t.Fatal("optional settings must remain unset")
	}

	failAtomically := func(t *testing.T, p Prompt, v PromptVersion, cause string) {
		t.Helper()
		oldP, oldV := p, v
		err := store.CreatePromptWithInitialVersion(ctx, &p, &v)
		if err == nil || !strings.Contains(err.Error(), cause) {
			t.Fatalf("expected creation to fail at %s: %v", cause, err)
		}
		if p != oldP || v.PromptID != oldV.PromptID || v.ID != oldV.ID || !v.CreatedAt.Equal(oldV.CreatedAt) ||
			v.ModelProfileVersionID != oldV.ModelProfileVersionID || v.GenerationConfig.Temperature != oldV.GenerationConfig.Temperature {
			t.Fatal("failed transaction wrote back its rows")
		}
		var hasRows bool
		err = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM prompts WHERE id=$1)
			OR EXISTS (SELECT 1 FROM prompt_versions WHERE prompt_id=$1)
			OR EXISTS (SELECT 1 FROM prompt_envs WHERE prompt_id=$1)`, uuid.MustParse(p.ID)).Scan(&hasRows)
		if err != nil || hasRows {
			t.Fatalf("failed create left partial rows: %v", err)
		}
	}
	t.Run("version insert fails", func(t *testing.T) {
		p, v := makeRecords()
		v.ModelProfileVersionID = uuid.NewString()
		failAtomically(t, p, v, "create initial prompt version")
	})
	t.Run("dev binding fails", func(t *testing.T) {
		exec("ALTER TABLE prompt_envs ADD CONSTRAINT reject_new_dev CHECK (env <> 'dev') NOT VALID")
		p, v := makeRecords()
		failAtomically(t, p, v, "bind initial prompt version to dev")
	})
}

func TestPromptReadAndAppend(t *testing.T) {
	pool := newPromptTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	p := Prompt{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Order check"}
	v1 := PromptVersion{ID: uuid.NewString(), PromptID: p.ID, Version: 1, Template: "Original {{.order_id}}", VariablesSchema: "{}"}
	if err := store.CreatePromptWithInitialVersion(ctx, &p, &v1); err != nil {
		t.Fatal(err)
	}
	parent, err := store.GetPrompt(ctx, "space-a", p.ID)
	if err != nil || parent.Name != p.Name || !parent.UpdatedAt.Equal(p.UpdatedAt) {
		t.Fatalf("get prompt does not match persisted row: %v", err)
	}
	version, err := store.GetPromptVersion(ctx, "space-a", v1.ID)
	if err != nil || version.Template != v1.Template || !version.CreatedAt.Equal(v1.CreatedAt) {
		t.Fatalf("get version does not match persisted row: %v", err)
	}
	if _, err := store.GetPrompt(ctx, "space-b", p.ID); !errors.Is(err, ErrPromptNotFound) {
		t.Fatalf("cross-workspace prompt lookup was not hidden: %v", err)
	}
	if _, err := store.GetPromptVersion(ctx, "space-b", v1.ID); !errors.Is(err, ErrPromptVersionNotFound) {
		t.Fatalf("cross-workspace version lookup was not hidden: %v", err)
	}
	list, err := store.ListPrompts(ctx, "space-a")
	if err != nil || len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("unexpected workspace list: %v", err)
	}
	emptyPrompts, err := store.ListPrompts(ctx, "space-b")
	if err != nil || emptyPrompts == nil || len(emptyPrompts) != 0 {
		t.Fatalf("workspace list leaked prompts or returned nil: %v", err)
	}
	emptyVersions, err := store.ListPromptVersions(ctx, "space-b", p.ID)
	if err != nil || emptyVersions == nil || len(emptyVersions) != 0 {
		t.Fatalf("workspace list leaked versions or returned nil: %v", err)
	}

	draft := PromptVersion{ID: uuid.NewString(), PromptID: p.ID, Version: 999, Template: "Updated {{.order_id}}", VariablesSchema: "{}"}
	if err := store.AppendPromptVersion(ctx, "space-b", &draft); !errors.Is(err, ErrPromptNotFound) || draft.Version != 999 || !draft.CreatedAt.IsZero() {
		t.Fatalf("cross-workspace append was accepted or mutated the draft: %v", err)
	}
	duplicate := draft
	duplicate.ID = v1.ID
	if err := store.AppendPromptVersion(ctx, "space-a", &duplicate); err == nil || duplicate.Version != 999 || !duplicate.CreatedAt.IsZero() {
		t.Fatalf("failed append was accepted or wrote back its draft: %v", err)
	}

	const writes = 6
	type result struct {
		version PromptVersion
		err     error
	}
	results := make(chan result, writes)
	start := make(chan struct{})
	for i := 0; i < writes; i++ {
		go func() {
			<-start
			next := draft
			next.ID = uuid.NewString()
			err := store.AppendPromptVersion(ctx, "space-a", &next)
			results <- result{version: next, err: err}
		}()
	}
	got := make(map[int]bool)
	close(start)
	for i := 0; i < writes; i++ {
		select {
		case result := <-results:
			if result.err != nil || result.version.Version < 2 || result.version.Version > writes+1 || got[result.version.Version] || result.version.CreatedAt.IsZero() {
				t.Fatalf("append must allocate a unique committed version, got %d: %v", result.version.Version, result.err)
			}
			got[result.version.Version] = true
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	versions, err := store.ListPromptVersions(ctx, "space-a", p.ID)
	if err != nil || len(versions) != writes+1 || versions[0].ID != v1.ID || versions[0].Template != v1.Template {
		t.Fatalf("appending unexpectedly changed version history: %v", err)
	}
	for i, version := range versions {
		if version.Version != i+1 {
			t.Fatalf("got version %d at index %d", version.Version, i)
		}
	}
	var dev uuid.UUID
	err = pool.QueryRow(ctx, "SELECT version_id FROM prompt_envs WHERE prompt_id=$1 AND env='dev'", uuid.MustParse(p.ID)).Scan(&dev)
	if err != nil || dev.String() != v1.ID {
		t.Fatalf("appending must not move dev: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT id FROM prompts WHERE id=$1 FOR UPDATE", uuid.MustParse(p.ID)); err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(ctx, 300*time.Millisecond)
	defer stop()
	err = store.AppendPromptVersion(wait, "space-a", &draft)
	if !errors.Is(err, context.DeadlineExceeded) || draft.Version != 999 || !draft.CreatedAt.IsZero() {
		t.Fatalf("append did not respect the held row lock or changed the draft: %v", err)
	}
}

func TestPromptEnvBinding(t *testing.T) {
	pool := newPromptTestPool(t)
	applyPromptMigration(t, pool, "000006_prompt_versions.up.sql")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	create := func(workspace string) (*Prompt, *PromptVersion) {
		t.Helper()
		p := &Prompt{ID: uuid.NewString(), WorkspaceID: workspace, Name: "Order check"}
		v := &PromptVersion{ID: uuid.NewString(), PromptID: p.ID, Version: 1, Template: "Check {{.order_id}}", VariablesSchema: "{}"}
		if err := store.CreatePromptWithInitialVersion(ctx, p, v); err != nil {
			t.Fatal(err)
		}
		return p, v
	}
	p, v1 := create("space-a")
	_, other := create("space-a")
	_, foreign := create("space-b")
	v2 := &PromptVersion{ID: uuid.NewString(), PromptID: p.ID, Template: "Track {{.order_id}}", VariablesSchema: "{}"}
	if err := store.AppendPromptVersion(ctx, "space-a", v2); err != nil {
		t.Fatal(err)
	}
	binding := func(env, versionID string) *EnvBinding {
		t.Helper()
		got, err := store.GetEnvBinding(ctx, "space-a", p.ID, env)
		if err != nil || got == nil || got.PromptID != p.ID || got.Env != env || got.VersionID != versionID || got.UpdatedAt.IsZero() {
			t.Fatalf("binding %q = %+v, want version %q: %v", env, got, versionID, err)
		}
		return got
	}
	binding("dev", v1.ID)
	for _, tc := range []struct {
		workspace, promptID, env string
		want                     error
	}{
		{"space-a", p.ID, "prod", ErrPromptEnvNotBound},
		{"space-b", p.ID, "dev", ErrPromptNotFound},
		{"space-a", uuid.NewString(), "dev", ErrPromptNotFound},
	} {
		if got, err := store.GetEnvBinding(ctx, tc.workspace, tc.promptID, tc.env); got != nil || !errors.Is(err, tc.want) {
			t.Fatalf("unavailable binding = %+v, %v, want %v", got, err, tc.want)
		}
	}
	if err := store.SetEnvBinding(ctx, "space-a", p.ID, "staging", v1.ID); err != nil {
		t.Fatal(err)
	}
	staging := binding("staging", v1.ID)
	if err := store.SetEnvBinding(ctx, "space-a", p.ID, "dev", v2.ID); err != nil {
		t.Fatal(err)
	}
	dev := binding("dev", v2.ID)
	for _, id := range []string{other.ID, foreign.ID, uuid.NewString()} {
		if err := store.SetEnvBinding(ctx, "space-a", p.ID, "dev", id); !errors.Is(err, ErrPromptVersionNotFound) {
			t.Fatalf("wrong target should be hidden: %v", err)
		}
	}
	if err := store.SetEnvBinding(ctx, "space-b", p.ID, "dev", v2.ID); !errors.Is(err, ErrPromptNotFound) {
		t.Fatalf("foreign prompt should be hidden: %v", err)
	}
	if !binding("dev", v2.ID).UpdatedAt.Equal(dev.UpdatedAt) || !binding("staging", v1.ID).UpdatedAt.Equal(staging.UpdatedAt) {
		t.Fatal("invalid writes or publishing to dev changed an unrelated binding")
	}
	if _, err := store.GetEnvBinding(ctx, "space-a", p.ID, "prod"); !errors.Is(err, ErrPromptEnvNotBound) {
		t.Fatalf("publishing elsewhere must not bind prod: %v", err)
	}
	for _, want := range []*PromptVersion{v1, v2} {
		got, err := store.GetPromptVersion(ctx, "space-a", want.ID)
		if err != nil || got == nil || got.Version != want.Version || got.Template != want.Template || got.Hash != want.Hash {
			t.Fatalf("publishing changed immutable version: %+v, %v", got, err)
		}
	}
}
