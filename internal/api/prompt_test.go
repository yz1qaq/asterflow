package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
)

func TestPromptHTTP(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	ctx := context.Background()
	for _, migration := range []string{
		"000003_model_accounts_deployments.up.sql", "000004_model_profiles.up.sql",
		"000005_project_model_bindings.up.sql", "000006_prompt_versions.up.sql",
		"000007_prompt_experiments.up.sql", "000008_prompt_env_events.up.sql",
	} {
		script, err := os.ReadFile("../../migrations/" + migration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(script)); err != nil {
			t.Fatal(err)
		}
	}

	identities := iam.NewService(iam.NewPostgresStore(pool), []byte("prompt-http-fixture-key-at-least-32-bytes"))
	createUser := func(email, name string) *domain.User {
		t.Helper()
		user, err := identities.CreateUser(ctx, email, "fixture-password123", name)
		if err != nil {
			t.Fatal(err)
		}
		return user
	}
	owner := createUser("owner@prompt.test", "Owner")
	viewer := createUser("viewer@prompt.test", "Viewer")
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
	tokenFor := func(email string) string {
		t.Helper()
		res, err := identities.Login(ctx, iam.LoginRequest{Email: email, Password: "fixture-password123"})
		if err != nil {
			t.Fatal(err)
		}
		return res.Token
	}
	ownToken, viewerToken := tokenFor(owner.Email), tokenFor(viewer.Email)

	models := modelconfig.NewService(modelconfig.NewPostgresStore(pool), nil)
	prompts := prompt.NewService(prompt.NewPostgresStore(pool), models)
	server := httptest.NewServer(NewRouter(pool, identities, models, prompts))
	defer server.Close()
	call := func(method, path, token, space, body string, want int) []byte {
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
			t.Fatalf("%s %s: HTTP status=%d want=%d read error=%v body=%s", method, path, res.StatusCode, want, err, got)
		}
		return got
	}
	marshal := func(data any) string {
		t.Helper()
		contents, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		return string(contents)
	}

	base := "/api/v1/prompts"
	call("GET", base, "", a, "", 401)
	call("GET", base, ownToken, "", "", 400)
	if string(call("GET", base, viewerToken, a, "", 200)) != "[]" {
		t.Fatal("empty list must be []")
	}

	body := marshal(map[string]any{
		"name": "Order check", "category": "system", "template": "Check {{.order_id}}",
		"variables_schema": `{"required":["order_id"]}`, "generation_config": map[string]any{"temperature": 0},
	})
	call("POST", base, viewerToken, a, body, 403)
	for _, bad := range []string{`{"workspace_id":"injected"}`, `{"created_by":"injected"}`,
		`{"name":"Bad","template":"{{.order_id"}`, "{} {}"} {
		call("POST", base, ownToken, a, bad, 400)
	}
	raw := call("POST", base, ownToken, a, body, 201)
	var created struct {
		Prompt  prompt.Prompt        `json:"prompt"`
		Version prompt.PromptVersion `json:"version"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.Prompt.WorkspaceID != a ||
		created.Prompt.CreatedBy != owner.ID || created.Version.CreatedBy != owner.ID ||
		created.Version.Version != 1 || created.Version.PromptID != created.Prompt.ID || created.Version.CreatedAt.IsZero() ||
		created.Version.GenerationConfig.Temperature == nil || *created.Version.GenerationConfig.Temperature != 0 {
		t.Fatal("created bundle must reflect authenticated fields and stored settings")
	}
	var listed []prompt.Prompt
	if err := json.Unmarshal(call("GET", base, viewerToken, a, "", 200), &listed); err != nil || len(listed) != 1 || listed[0].ID != created.Prompt.ID {
		t.Fatal("viewer should see exactly the stored prompt")
	}
	if string(call("GET", base, ownToken, b, "", 200)) != "[]" {
		t.Fatal("cross-workspace list leaked the prompt")
	}

	path := base + "/" + created.Prompt.ID
	versionBody := `{"template":"Track {{.order_id}}","variables_schema":"{\"required\":[\"order_id\"]}"}`
	call("POST", path+"/versions", viewerToken, a, versionBody, 403)
	call("POST", path+"/versions", ownToken, b, versionBody, 404)
	call("GET", path+"/versions", ownToken, b, "", 404)
	call("GET", base+"/invalid/versions", ownToken, a, "", 400)
	call("POST", path+"/versions", ownToken, a, `{"template":"x","version":99}`, 400)

	var v2 prompt.PromptVersion
	raw = call("POST", path+"/versions", ownToken, a, versionBody, 201)
	if err := json.Unmarshal(raw, &v2); err != nil || v2.Version != 2 || v2.PromptID != created.Prompt.ID || v2.CreatedBy != owner.ID {
		t.Fatal("append should write v2 in the same prompt")
	}
	var history []prompt.PromptVersion
	if err := json.Unmarshal(call("GET", path+"/versions", viewerToken, a, "", 200), &history); err != nil ||
		len(history) != 2 || history[0].ID != created.Version.ID || history[1].ID != v2.ID {
		t.Fatal("viewer must be able to read the two ordered versions")
	}
	var dev string
	if err := pool.QueryRow(ctx, "SELECT version_id::text FROM prompt_envs WHERE prompt_id=$1 AND env='dev'", uuid.MustParse(created.Prompt.ID)).Scan(&dev); err != nil || dev != created.Version.ID {
		t.Fatalf("HTTP version append did not preserve dev: %v", err)
	}
	envBindings := func(status int) []prompt.EnvBinding {
		t.Helper()
		var list []prompt.EnvBinding
		if err := json.Unmarshal(call("GET", path+"/envs", viewerToken, a, "", status), &list); err != nil {
			t.Fatal(err)
		}
		return list
	}
	if binding := envBindings(200); len(binding) != 1 || binding[0].PromptID != created.Prompt.ID ||
		binding[0].Env != "dev" || binding[0].VersionID != created.Version.ID || binding[0].UpdatedAt.IsZero() {
		t.Fatalf("viewer should read the single dev binding: %+v", binding)
	}
	call("GET", path+"/envs", ownToken, b, "", 404)
	call("GET", base+"/invalid/envs", ownToken, a, "", 400)
	call("GET", base+"/"+uuid.NewString()+"/envs", ownToken, a, "", 404)

	render := func(id, order string) string {
		return marshal(map[string]any{"version_id": id, "vars": map[string]any{"order_id": order}})
	}
	for id, want := range map[string]string{created.Version.ID: "Check ORD-1001", v2.ID: "Track ORD-1001"} {
		raw = call("POST", path+"/render", ownToken, a, render(id, "ORD-1001"), 200)
		var out struct{ Rendered string }
		if err := json.Unmarshal(raw, &out); err != nil || out.Rendered != want {
			t.Fatalf("preview did not use the requested version: %v", err)
		}
	}
	if raw = call("POST", path+"/render", ownToken, a, marshal(map[string]any{"version_id": v2.ID, "vars": map[string]any{}}), 400); !strings.Contains(string(raw), "order_id") {
		t.Fatal("preview missing-variable error must identify the variable")
	}
	call("POST", path+"/render", viewerToken, a, render(v2.ID, "ORD-1001"), 403)
	call("POST", path+"/render", ownToken, b, render(v2.ID, "ORD-1001"), 404)
	call("POST", base+"/"+uuid.NewString()+"/render", ownToken, a, render(v2.ID, "ORD-1001"), 404)

	envRender := func(env, order string) string {
		body := map[string]any{"vars": map[string]any{"order_id": order}}
		if env != "" {
			body["env"] = env
		}
		return marshal(body)
	}
	expectRender := func(method, space, request, want string) {
		t.Helper()
		var out struct{ Rendered string }
		if err := json.Unmarshal(call(method, path+"/render", ownToken, space, request, 200), &out); err != nil || out.Rendered != want {
			t.Fatalf("env preview = %q, want %q: %v", out.Rendered, want, err)
		}
	}
	expectRender("POST", a, envRender("", "ORD-1001"), "Check ORD-1001")
	expectRender("POST", a, envRender("dev", "ORD-1001"), "Check ORD-1001")
	call("POST", path+"/render", ownToken, a, marshal(map[string]any{"version_id": v2.ID, "env": "dev", "vars": map[string]any{"order_id": "ORD-1001"}}), 400)
	call("POST", path+"/render", ownToken, a, envRender("prod", "ORD-1001"), 404)
	call("POST", path+"/render", ownToken, b, envRender("dev", "ORD-1001"), 404)

	var sibling struct {
		Prompt  prompt.Prompt        `json:"prompt"`
		Version prompt.PromptVersion `json:"version"`
	}
	if err := json.Unmarshal(call("POST", base, ownToken, a, marshal(map[string]any{"name": "Other", "template": "Other {{.order_id}}"}), 201), &sibling); err != nil {
		t.Fatal(err)
	}
	promote := func(env, versionID string) string {
		return marshal(map[string]any{"env": env, "version_id": versionID})
	}
	devVersion := func() string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, "SELECT version_id::text FROM prompt_envs WHERE prompt_id=$1 AND env='dev'", uuid.MustParse(created.Prompt.ID)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	call("POST", path+"/promote", viewerToken, a, promote("dev", v2.ID), 403)
	call("POST", path+"/promote", ownToken, a, `{"env":"dev","version_id":"v2"}`, 400)
	call("POST", path+"/promote", ownToken, a, promote(" ", v2.ID), 400)
	call("POST", path+"/promote", ownToken, b, promote("dev", v2.ID), 404)
	call("POST", path+"/promote", ownToken, a, promote("dev", uuid.NewString()), 404)
	call("POST", path+"/promote", ownToken, a, promote("dev", sibling.Version.ID), 404)
	if got := devVersion(); got != created.Version.ID {
		t.Fatalf("invalid promotes moved dev to %s", got)
	}

	if raw = call("POST", path+"/promote", ownToken, a, promote("staging", created.Version.ID), 200); string(raw) != `{"status":"promoted"}` {
		t.Fatalf("promote response = %s", raw)
	}
	expectRender("POST", a, envRender("staging", "ORD-1001"), "Check ORD-1001")
	expectRender("POST", a, envRender("", "ORD-1001"), "Check ORD-1001")
	if got := devVersion(); got != created.Version.ID {
		t.Fatalf("publishing to staging moved dev to %s", got)
	}

	call("POST", path+"/promote", ownToken, a, promote("dev", v2.ID), 200)
	if got := devVersion(); got != v2.ID {
		t.Fatalf("explicit promote did not move dev to v2, got %s", got)
	}
	expectRender("POST", a, envRender("", "ORD-1001"), "Track ORD-1001")
	expectRender("POST", a, envRender("dev", "ORD-1001"), "Track ORD-1001")
	expectRender("POST", a, envRender("staging", "ORD-1001"), "Check ORD-1001")
	call("POST", path+"/render", ownToken, a, envRender("prod", "ORD-1001"), 404)
	expectRender("POST", a, render(created.Version.ID, "ORD-1001"), "Check ORD-1001")

	if binding := envBindings(200); len(binding) != 2 ||
		binding[0].Env != "dev" || binding[0].VersionID != v2.ID ||
		binding[1].Env != "staging" || binding[1].VersionID != created.Version.ID {
		t.Fatalf("environment pointers = %+v", binding)
	}

	call("POST", path+"/rollback", viewerToken, a, promote("dev", created.Version.ID), 403)
	call("POST", path+"/rollback", ownToken, a, `{"env":"dev","version_id":"v2"}`, 400)
	call("POST", path+"/rollback", ownToken, b, promote("dev", created.Version.ID), 404)
	call("POST", path+"/rollback", ownToken, a, promote("dev", sibling.Version.ID), 404)
	if got := devVersion(); got != v2.ID {
		t.Fatalf("invalid rollbacks moved dev to %s", got)
	}
	if raw = call("POST", path+"/rollback", ownToken, a, promote("dev", created.Version.ID), 200); string(raw) != `{"status":"rolled_back"}` {
		t.Fatalf("rollback response = %s", raw)
	}
	if got := devVersion(); got != created.Version.ID {
		t.Fatalf("rollback did not restore dev to v1, got %s", got)
	}
	type envEvent struct {
		action, actor string
		from, to      sql.NullString
	}
	readEnvEvents := func(env string) []envEvent {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT action, actor, from_version_id::text, to_version_id::text
			FROM prompt_env_events WHERE prompt_id=$1 AND env=$2 ORDER BY created_at, id`,
			uuid.MustParse(created.Prompt.ID), env)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var events []envEvent
		for rows.Next() {
			var item envEvent
			if err := rows.Scan(&item.action, &item.actor, &item.from, &item.to); err != nil {
				t.Fatal(err)
			}
			events = append(events, item)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return events
	}
	if events := readEnvEvents("dev"); len(events) != 2 ||
		events[0].action != "published" || !events[0].from.Valid || events[0].from.String != created.Version.ID ||
		events[0].to.String != v2.ID || events[0].actor != owner.ID ||
		events[1].action != "rolled_back" || !events[1].from.Valid || events[1].from.String != v2.ID ||
		events[1].to.String != created.Version.ID || events[1].actor != owner.ID {
		t.Fatalf("dev environment events = %+v", events)
	}
	if events := readEnvEvents("staging"); len(events) != 1 ||
		events[0].action != "published" || events[0].from.Valid || events[0].to.String != created.Version.ID {
		t.Fatalf("staging environment events = %+v", events)
	}
	envEventsHTTP := func(space, env string) []domain.PromptEnvEvent {
		t.Helper()
		var events []domain.PromptEnvEvent
		if err := json.Unmarshal(call("GET", path+"/env-events?env="+env, viewerToken, space, "", 200), &events); err != nil {
			t.Fatal(err)
		}
		return events
	}
	if events := envEventsHTTP(a, "dev"); len(events) != 2 ||
		events[0].Action != "rolled_back" || events[0].FromVersionID != v2.ID || events[0].ToVersionID != created.Version.ID ||
		events[0].Actor != owner.ID || events[0].CreatedAt.IsZero() ||
		events[1].Action != "published" || events[1].FromVersionID != created.Version.ID || events[1].ToVersionID != v2.ID {
		t.Fatalf("HTTP dev env events = %+v", events)
	}
	if events := envEventsHTTP(a, "staging"); len(events) != 1 ||
		events[0].FromVersionID != "" || events[0].ToVersionID != created.Version.ID {
		t.Fatalf("HTTP staging env events = %+v", events)
	}
	call("GET", path+"/env-events?env=dev", ownToken, b, "", 404)
	call("GET", base+"/invalid/env-events?env=dev", ownToken, a, "", 400)
	call("GET", path+"/env-events", ownToken, a, "", 400)
	if binding := envBindings(200); len(binding) != 2 ||
		binding[0].Env != "dev" || binding[0].VersionID != created.Version.ID ||
		binding[1].Env != "staging" || binding[1].VersionID != created.Version.ID {
		t.Fatalf("rollback pointers = %+v", binding)
	}
	expectRender("POST", a, envRender("", "ORD-1001"), "Check ORD-1001")
	expectRender("POST", a, render(v2.ID, "ORD-1001"), "Track ORD-1001")

	diffText := func(from, to int) string {
		t.Helper()
		var out struct{ Diff string }
		if err := json.Unmarshal(call("GET", path+"/diff?from="+strconv.Itoa(from)+"&to="+strconv.Itoa(to), viewerToken, a, "", 200), &out); err != nil {
			t.Fatal(err)
		}
		return out.Diff
	}
	if got := diffText(1, 2); got != "- Check {{.order_id}}\n+ Track {{.order_id}}\n" {
		t.Fatalf("forward diff = %q", got)
	}
	if got := diffText(2, 1); got != "- Track {{.order_id}}\n+ Check {{.order_id}}\n" {
		t.Fatalf("reverse diff = %q", got)
	}
	var v3 prompt.PromptVersion
	if err := json.Unmarshal(call("POST", path+"/versions", ownToken, a,
		`{"template":"Track {{.order_id}}","variables_schema":"{\"required\":[\"order_id\",\"priority\"]}"}`, 201), &v3); err != nil {
		t.Fatal(err)
	}
	if got := diffText(2, 3); !strings.HasPrefix(got, "--- template\n") || !strings.Contains(got, "--- variables_schema\n") ||
		!strings.Contains(got, `+ {"required":["order_id","priority"]}`) {
		t.Fatalf("schema diff = %q", got)
	}
	call("GET", path+"/diff?from=1&to=99", ownToken, a, "", 404)
	call("GET", path+"/diff?from=1&to=2", ownToken, b, "", 404)
	call("GET", path+"/diff?from=0&to=2", ownToken, a, "", 400)
	call("GET", path+"/diff?from=x&to=2", ownToken, a, "", 400)
	call("GET", path+"/diff", ownToken, a, "", 400)

	activeExperiment := func(space string, want int) []byte {
		t.Helper()
		return call("GET", path+"/experiments/active?env=dev", viewerToken, space, "", want)
	}
	if raw = activeExperiment(a, 200); string(raw) != "null" {
		t.Fatalf("no active experiment should be null: %s", raw)
	}
	call("GET", path+"/experiments/active?env=dev", ownToken, b, "", 404)
	call("GET", base+"/invalid/experiments/active", ownToken, a, "", 400)
	call("GET", path+"/experiments/"+uuid.NewString()+"/events", ownToken, a, "", 404)
	call("GET", base+"/invalid/experiments/anything/events", ownToken, a, "", 400)

	startRolloutBody := func(candidate string, traffic int) string {
		return marshal(map[string]any{"env": "dev", "candidate_version_id": candidate, "traffic": traffic})
	}
	call("POST", path+"/rollouts", viewerToken, a, startRolloutBody(v2.ID, 20), 403)
	call("POST", path+"/rollouts", ownToken, a, startRolloutBody(v2.ID, 100), 400)
	call("POST", path+"/rollouts", ownToken, a, startRolloutBody(created.Version.ID, 20), 400)
	call("POST", path+"/rollouts", ownToken, b, startRolloutBody(v2.ID, 20), 404)
	var rollout domain.PromptExperiment
	if err := json.Unmarshal(call("POST", path+"/rollouts", ownToken, a, startRolloutBody(v2.ID, 20), 201), &rollout); err != nil ||
		rollout.Status != "active" || rollout.CandidateVersionID != v2.ID ||
		rollout.TrafficPercent != 20 || rollout.BaselineVersionID != created.Version.ID {
		t.Fatalf("start rollout = %+v, %v", rollout, err)
	}
	call("POST", path+"/rollouts", ownToken, a, startRolloutBody(v2.ID, 10), 409)
	var current domain.PromptExperiment
	if err := json.Unmarshal(activeExperiment(a, 200), &current); err != nil ||
		current.ID != rollout.ID || current.Status != "active" || current.TrafficPercent != 20 || current.CandidateVersionID != v2.ID {
		t.Fatalf("active experiment read = %+v, %v", current, err)
	}

	renderFor := func(token string) string {
		t.Helper()
		var out struct{ Rendered string }
		if err := json.Unmarshal(call("POST", path+"/render", token, a,
			marshal(map[string]any{"env": "dev", "vars": map[string]any{"order_id": "ORD-1001"}}), 200), &out); err != nil {
			t.Fatal(err)
		}
		return out.Rendered
	}
	bucketText := func(userID string) string {
		if int(crc32.ChecksumIEEE([]byte(userID+rollout.ID))%100) < 20 {
			return "Track ORD-1001"
		}
		return "Check ORD-1001"
	}
	if got := renderFor(ownToken); got != bucketText(owner.ID) {
		t.Fatalf("owner render %q, want bucket %q", got, bucketText(owner.ID))
	}

	call("PUT", path+"/rollouts/traffic", viewerToken, a, `{"env":"dev","traffic":50}`, 403)
	var updated domain.PromptExperiment
	if err := json.Unmarshal(call("PUT", path+"/rollouts/traffic", ownToken, a, `{"env":"dev","traffic":50}`, 200), &updated); err != nil ||
		updated.TrafficPercent != 50 || updated.Variants[0].Traffic != 50 {
		t.Fatalf("update traffic = %+v, %v", updated, err)
	}

	call("POST", path+"/rollouts/complete", viewerToken, a, `{"env":"dev"}`, 403)
	if raw = call("POST", path+"/rollouts/complete", ownToken, a, `{"env":"dev"}`, 200); string(raw) != `{"status":"completed"}` {
		t.Fatalf("complete response = %s", raw)
	}
	if got := devVersion(); got != v2.ID {
		t.Fatalf("completion did not promote the candidate, dev=%s", got)
	}
	if got := renderFor(ownToken); got != "Track ORD-1001" {
		t.Fatalf("after completion every user should use the candidate: %q", got)
	}
	if raw = activeExperiment(a, 200); string(raw) != "null" {
		t.Fatalf("completed rollout must not be active: %s", raw)
	}
	if events := envEventsHTTP(a, "dev"); len(events) != 3 ||
		events[0].Action != "published" || events[0].FromVersionID != created.Version.ID ||
		events[0].ToVersionID != v2.ID || events[0].Detail != "rollout completed" {
		t.Fatalf("completion environment event = %+v", events)
	}
	readEvents := func(space string, want int) []domain.PromptRolloutEvent {
		t.Helper()
		var events []domain.PromptRolloutEvent
		if err := json.Unmarshal(call("GET", path+"/experiments/"+rollout.ID+"/events", viewerToken, space, "", want), &events); err != nil {
			t.Fatal(err)
		}
		return events
	}
	events := readEvents(a, 200)
	if len(events) != 3 || events[0].Action != "started" || events[0].ToTraffic != 20 ||
		events[1].Action != "traffic_changed" || events[1].FromTraffic != 20 || events[1].ToTraffic != 50 ||
		events[2].Action != "completed" || events[2].FromTraffic != 50 || events[2].ToTraffic != 100 ||
		events[2].Actor != owner.ID || events[2].ExperimentID != rollout.ID || events[2].CreatedAt.IsZero() {
		t.Fatalf("rollout timeline = %+v", events)
	}
	call("GET", path+"/experiments/"+rollout.ID+"/events", ownToken, b, "", 404)

	var second domain.PromptExperiment
	if err := json.Unmarshal(call("POST", path+"/rollouts", ownToken, a, startRolloutBody(created.Version.ID, 10), 201), &second); err != nil {
		t.Fatal(err)
	}
	call("POST", path+"/rollouts/rollback", viewerToken, a, `{"env":"dev"}`, 403)
	if raw = call("POST", path+"/rollouts/rollback", ownToken, a, `{"env":"dev"}`, 200); string(raw) != `{"status":"rolled_back"}` {
		t.Fatalf("rollback response = %s", raw)
	}
	if got := devVersion(); got != v2.ID {
		t.Fatalf("rollback must keep the promoted pointer, dev=%s", got)
	}
	call("POST", path+"/rollouts/complete", ownToken, a, `{"env":"dev"}`, 400)

	var experiment domain.PromptExperiment
	if err := json.Unmarshal(call("POST", path+"/experiments", ownToken, a, marshal(map[string]any{
		"env": "dev", "variants": []map[string]any{
			{"version_id": created.Version.ID, "traffic": 30}, {"version_id": v2.ID, "traffic": 30},
		}}), 201), &experiment); err != nil || len(experiment.Variants) != 2 {
		t.Fatalf("start experiment = %+v, %v", experiment, err)
	}
	multiEvents := func() []domain.PromptRolloutEvent {
		t.Helper()
		var events []domain.PromptRolloutEvent
		if err := json.Unmarshal(call("GET", path+"/experiments/"+experiment.ID+"/events", viewerToken, a, "", 200), &events); err != nil {
			t.Fatal(err)
		}
		return events
	}
	if events := multiEvents(); len(events) != 1 || events[0].Action != "started" ||
		events[0].ToTraffic != 60 || !strings.Contains(events[0].Detail, "multi-variant") {
		t.Fatalf("multi-variant start event = %+v", events)
	}
	call("PUT", path+"/rollouts/traffic", ownToken, a, `{"env":"dev","traffic":40}`, 400)
	call("POST", path+"/rollouts/complete", ownToken, a, `{"env":"dev"}`, 400)
	if raw = call("POST", path+"/rollouts/rollback", ownToken, a, `{"env":"dev"}`, 200); string(raw) != `{"status":"rolled_back"}` {
		t.Fatalf("stop experiment response = %s", raw)
	}
	if events := multiEvents(); len(events) != 2 || events[1].Action != "rolled_back" ||
		events[1].FromTraffic != 60 || events[1].ToTraffic != 0 || !strings.Contains(events[1].Detail, "multi-variant") {
		t.Fatalf("multi-variant stop event = %+v", events)
	}
	if got := devVersion(); got != v2.ID {
		t.Fatalf("stopping an experiment must not move the pointer, dev=%s", got)
	}
}
