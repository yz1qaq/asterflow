package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
)

func TestToolRegistryHTTP(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	ctx := context.Background()
	for _, migrationName := range []string{"000012_tool_registry.up.sql", "000013_tool_test_runs.up.sql"} {
		migration, err := os.ReadFile("../../migrations/" + migrationName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}

	iamService := iam.NewService(iam.NewPostgresStore(pool), []byte("tool-http-fixture-key-at-least-32-bytes"))
	const password = "fixture-password123"
	createUser := func(email, name string) *domain.User {
		t.Helper()
		user, err := iamService.CreateUser(ctx, email, password, name)
		if err != nil {
			t.Fatal(err)
		}
		return user
	}
	owner := createUser("owner@tools.test", "Owner")
	viewer := createUser("viewer@tools.test", "Viewer")
	createSpace := func(name string) string {
		t.Helper()
		space, err := iamService.CreateWorkspaceForUser(ctx, owner.ID, name, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return space.ID
	}
	a, b := createSpace("A"), createSpace("B")
	if _, err := iamService.UpsertWorkspaceMember(ctx, owner.ID, a, viewer.ID, iam.WorkspaceRoleViewer); err != nil {
		t.Fatal(err)
	}
	tokenFor := func(user *domain.User) string {
		t.Helper()
		login, err := iamService.Login(ctx, iam.LoginRequest{Email: user.Email, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		return login.Token
	}
	ownerToken, viewerToken := tokenFor(owner), tokenFor(viewer)

	toolID := uuid.NewString()
	store := tool.NewPostgresStore(pool)
	if err := store.CreateToolWithVersion(ctx, &domain.Tool{
		ID: toolID, WorkspaceID: a, Name: "get_order", SourceType: "rest_api", CreatedBy: owner.ID,
	}, &domain.ToolVersion{
		ID: uuid.NewString(), ToolID: toolID, Version: 1, Status: "draft",
		SchemaJSON: "{}", EndpointConfig: "{}", RetryPolicy: "{}",
		AuthConfig: `{"token":"fixture-only-value"}`, AuthConfigEncrypted: []byte{1, 2, 3},
	}); err != nil {
		t.Fatal(err)
	}

	cipher, err := modelconfig.NewCipher([]byte("tool-http-fixture-secret"))
	if err != nil {
		t.Fatal(err)
	}
	toolsService := tool.NewService(store)
	policy := tool.NewEndpointPolicy([]string{"127.0.0.1"}, false)
	toolsService.ConfigureSecurity(cipher, policy)
	registry, err := tooling.NewRegistry(policy)
	if err != nil {
		t.Fatal(err)
	}
	trialID, trialVersionID := uuid.NewString(), uuid.NewString()
	const querySchema = `{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}}}`
	var sdkCalls atomic.Int32
	registry.RegisterSDK("orders_for_trial", tooling.ExecutorFunc(func(runCtx context.Context, args json.RawMessage) (string, error) {
		sdkCalls.Add(1)
		// Simulate a draft append while the selected version is executing.
		if _, err := toolsService.CreateToolVersion(runCtx, a, trialID, tool.CreateToolVersionRequest{}); err != nil {
			return "", err
		}
		return string(args), nil
	}))
	server := httptest.NewServer(NewRouter(pool, iamService, nil, nil, Services{Tool: toolsService, ToolRegistry: registry}))
	defer server.Close()
	request := func(method, path, payload, token, space string, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(payload))
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
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("%s %s status %d, want %d: %s (%v)", method, path, res.StatusCode, want, body, err)
		}
		return body
	}

	get := func(path, token, space string, want int) []byte {
		t.Helper()
		return request(http.MethodGet, path, "", token, space, want)
	}

	const toolsPath = "/api/v1/tools"
	get(toolsPath, "", a, http.StatusUnauthorized)
	get(toolsPath, ownerToken, "", http.StatusBadRequest)
	get(toolsPath, viewerToken, b, http.StatusForbidden)

	var tools []domain.Tool
	if err := json.Unmarshal(get(toolsPath, viewerToken, a, http.StatusOK), &tools); err != nil ||
		len(tools) != 1 || tools[0].ID != toolID {
		t.Fatalf("unexpected space A tool list: %+v, %v", tools, err)
	}
	if body := get(toolsPath, ownerToken, b, http.StatusOK); string(body) != "[]" {
		t.Fatalf("want empty space B tool list, got %s", body)
	}

	versionsPath := toolsPath + "/" + toolID + "/versions"
	get(versionsPath, ownerToken, b, http.StatusNotFound)
	get(toolsPath+"/bad-id/versions", ownerToken, a, http.StatusNotFound)
	var versions []map[string]any
	if err := json.Unmarshal(get(versionsPath, viewerToken, a, http.StatusOK), &versions); err != nil || len(versions) != 1 {
		t.Fatalf("unexpected version list: %+v, %v", versions, err)
	}
	version := versions[0]
	_, hasCipher := version["auth_config_encrypted"]
	if version["auth_config"] != "{}" || version["has_auth"] != true || hasCipher ||
		version["status"] != "draft" || version["version"] != float64(1) {
		t.Fatal("version response must include draft status and sanitized authentication metadata")
	}
	const body = `{"name":"lookup_order","source_type":"internal_sdk","endpoint_config":"{\"sdk_name\":\"orders\"}","auth_config":"{\"token\":\"fixture-only\"}"}`
	post := func(body, token, space string, want int) []byte {
		t.Helper()
		return request(http.MethodPost, toolsPath, body, token, space, want)
	}
	post(body, viewerToken, a, http.StatusForbidden)
	post(`{"workspace_id":"other","created_by":"other"}`, ownerToken, a, http.StatusBadRequest)
	post(`{"name":"bad_endpoint","source_type":"rest_api"}`, ownerToken, a, http.StatusBadRequest)

	var created domain.Tool
	if err := json.Unmarshal(post(body, ownerToken, a, http.StatusCreated), &created); err != nil ||
		created.Name != "lookup_order" || created.WorkspaceID != a || created.CreatedBy != owner.ID {
		t.Fatalf("wrong created tool: %+v, %v", created, err)
	}
	post(body, ownerToken, a, http.StatusConflict)
	post(body, ownerToken, b, http.StatusCreated)

	var drafts []domain.ToolVersion
	path := toolsPath + "/" + created.ID + "/versions"
	if err := json.Unmarshal(get(path, viewerToken, a, http.StatusOK), &drafts); err != nil ||
		len(drafts) != 1 || drafts[0].ToolID != created.ID || drafts[0].Version != 1 ||
		drafts[0].Status != "draft" || drafts[0].AuthConfig != "{}" || !drafts[0].HasAuth {
		t.Fatal("created tool must have a single sanitized v1 draft")
	}
	t.Run("reject append based on cleared credentials", func(t *testing.T) {
		item, err := toolsService.CreateTool(ctx, tool.CreateToolRequest{
			WorkspaceID: a, CreatedBy: owner.ID, Name: "concurrent_credentials", SourceType: "internal_sdk",
			EndpointConfig: `{"sdk_name":"fixture"}`, AuthConfig: `{"token":"fixture-old"}`,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Hold the stale request after it reads v1, then commit a second request.
		readVersion := make(chan struct{})
		allowAppend := make(chan struct{})
		var resume sync.Once
		resumeAppend := func() { resume.Do(func() { close(allowAppend) }) }
		slowStore := delayedToolVersionStore{Store: store, read: readVersion, proceed: allowAppend}
		staleService := tool.NewService(slowStore)
		staleService.ConfigureSecurity(cipher, policy)
		staleServer := httptest.NewServer(NewRouter(pool, iamService, nil, nil, Services{Tool: staleService, ToolRegistry: registry}))
		defer staleServer.Close()
		defer resumeAppend()

		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		path := toolsPath + "/" + item.ID + "/versions"
		staleRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, staleServer.URL+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		staleRequest.Header.Set("Content-Type", "application/json")
		staleRequest.Header.Set("Authorization", "Bearer "+ownerToken)
		staleRequest.Header.Set("X-Workspace-ID", a)
		type reply struct {
			status int
			body   []byte
			err    error
		}
		result := make(chan reply, 1)
		go func() {
			response, err := staleServer.Client().Do(staleRequest)
			if err != nil {
				result <- reply{err: err}
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			result <- reply{status: response.StatusCode, body: body, err: err}
		}()

		select {
		case <-readVersion:
		case <-requestCtx.Done():
			t.Fatal("stale request did not read v1")
		}
		request(http.MethodPost, path, `{"auth_config":"{}"}`, ownerToken, a, http.StatusCreated)
		resumeAppend()

		got := <-result
		if got.err != nil || got.status != http.StatusConflict || !strings.Contains(string(got.body), "tool_version_conflict") {
			t.Fatalf("want 409 and a version conflict, got %d %s (%v)", got.status, got.body, got.err)
		}
		latest, err := store.GetToolCurrentVersion(ctx, item.ID)
		if err != nil || latest.Version != 2 || latest.HasAuth {
			t.Fatalf("want cleared v2 with no extra version: %+v (%v)", latest, err)
		}
		request(http.MethodPost, path, `{}`, ownerToken, a, http.StatusCreated)
		retried, err := store.GetToolCurrentVersion(ctx, item.ID)
		if err != nil || retried.Version != 3 || retried.HasAuth {
			t.Fatalf("retry should only inherit cleared v2: %+v (%v)", retried, err)
		}
	})
	t.Run("append draft", func(t *testing.T) {
		appendVersion := func(body, token, space string, want int) []byte {
			t.Helper()
			return request(http.MethodPost, path, body, token, space, want)
		}
		appendVersion("{}", viewerToken, a, http.StatusForbidden)
		appendVersion("{}", ownerToken, b, http.StatusNotFound)
		appendVersion(`{"created_by":"spoofed"}`, ownerToken, a, http.StatusBadRequest)
		appendVersion(`{"retry_policy":"[]"}`, ownerToken, a, http.StatusBadRequest)
		appendVersion(`{"schema_json":"[]"}`, ownerToken, a, http.StatusBadRequest)

		initial := drafts[0]
		var second domain.ToolVersion
		response := appendVersion(`{"endpoint_config":"{\"sdk_name\":\"orders_v2\"}"}`, ownerToken, a, http.StatusCreated)
		if err := json.Unmarshal(response, &second); err != nil ||
			second.ID == initial.ID || second.ToolID != created.ID || second.Version != 2 ||
			second.Status != "draft" || second.CreatedAt.IsZero() || second.CreatedBy != owner.ID ||
			second.SchemaJSON != initial.SchemaJSON || second.RetryPolicy != initial.RetryPolicy ||
			second.EndpointConfig != `{"sdk_name":"orders_v2"}` ||
			!second.HasAuth || second.AuthConfig != "{}" {
			t.Fatalf("wrong appended draft: %+v (%v)", second, err)
		}
		if strings.Contains(string(response), "fixture-only") || strings.Contains(string(response), "auth_config_encrypted") {
			t.Fatal("append response exposed credentials")
		}
		current, err := store.GetToolCurrentVersion(ctx, created.ID)
		if err != nil || current.ID != second.ID || len(current.AuthConfigEncrypted) == 0 || current.AuthConfig != "{}" {
			t.Fatalf("new current version is missing its ciphertext: %v", err)
		}
		plain, err := cipher.Decrypt(current.AuthConfigEncrypted)
		if err != nil || plain != `{"token":"fixture-only"}` {
			t.Fatal("stored v2 must inherit the original credential")
		}

		var third domain.ToolVersion
		response = appendVersion(`{"auth_config":"{ }"}`, ownerToken, a, http.StatusCreated)
		if err := json.Unmarshal(response, &third); err != nil || third.Version != 3 ||
			third.EndpointConfig != second.EndpointConfig || third.HasAuth {
			t.Fatalf("cleared v3 should keep v2 endpoint with no authentication: %+v (%v)", third, err)
		}

		var all []domain.ToolVersion
		if err := json.Unmarshal(get(path, viewerToken, a, http.StatusOK), &all); err != nil || len(all) != 3 {
			t.Fatalf("expected three versions: %+v (%v)", all, err)
		}
		if all[2].ID != initial.ID || all[2].EndpointConfig != initial.EndpointConfig || !all[2].HasAuth ||
			!all[1].HasAuth || all[0].HasAuth {
			t.Fatal("appending a draft changed existing version content")
		}
	})
	t.Run("test selected version", func(t *testing.T) {
		if err := store.CreateToolWithVersion(ctx, &domain.Tool{
			ID: trialID, WorkspaceID: a, Name: "trial_fixture", SourceType: "internal_sdk", CreatedBy: owner.ID,
		}, &domain.ToolVersion{
			ID: trialVersionID, ToolID: trialID, Status: "draft", SchemaJSON: querySchema,
			EndpointConfig: `{"sdk_name":"orders_for_trial"}`, AuthConfig: "{}", RetryPolicy: "{}",
		}); err != nil {
			t.Fatal(err)
		}
		trialPath := toolsPath + "/" + trialID + "/test"
		trial := func(body, token, space string, want int) []byte {
			t.Helper()
			return request(http.MethodPost, trialPath, body, token, space, want)
		}
		const input = `{"input":{"order_id":"fixture-order"}}`
		trial("{}", ownerToken, a, http.StatusBadRequest)
		trial(input, viewerToken, a, http.StatusForbidden)
		trial(input, ownerToken, b, http.StatusNotFound)
		var invalid domain.ToolTestRun
		if err := json.Unmarshal(trial(`{"input":{}}`, ownerToken, a, http.StatusOK), &invalid); err != nil ||
			invalid.ToolVersionID != trialVersionID || invalid.Status != "error" ||
			invalid.Error == nil || !strings.Contains(*invalid.Error, "order_id") || sdkCalls.Load() != 0 {
			t.Fatalf("invalid schema must record failure without execution: %+v (%v)", invalid, err)
		}
		var success domain.ToolTestRun
		if err := json.Unmarshal(trial(input, ownerToken, a, http.StatusOK), &success); err != nil ||
			success.Status != "success" || success.ToolVersionID != trialVersionID || success.ToolID != trialID ||
			success.Output != `{"order_id":"fixture-order"}` || success.CreatedAt.IsZero() || sdkCalls.Load() != 1 {
			t.Fatalf("trial must run and persist against selected v1: %+v (%v)", success, err)
		}
		current, err := store.GetToolCurrentVersion(ctx, trialID)
		if err != nil || current.Version != 2 || current.ID == trialVersionID {
			t.Fatalf("trial should coexist with new draft: %+v (%v)", current, err)
		}
		if _, err := store.GetToolLastSuccessfulTestRunForVersion(ctx, current.ID); !errors.Is(err, tool.ErrNoSuccessfulTestRun) {
			t.Fatalf("v2 cannot claim the v1 run: %v", err)
		}
		stored, err := store.GetToolLastSuccessfulTestRunForVersion(ctx, trialVersionID)
		if err != nil || stored.ID != success.ID {
			t.Fatalf("trial was not recorded for v1: %+v (%v)", stored, err)
		}

		sensitive, err := toolsService.CreateTool(ctx, tool.CreateToolRequest{
			WorkspaceID: a, CreatedBy: owner.ID, Name: "sensitive_fixture", SourceType: "internal_sdk",
			EndpointConfig: `{"sdk_name":"orders_for_trial"}`, Sensitive: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		request(http.MethodPost, toolsPath+"/"+sensitive.ID+"/test", input, ownerToken, a, http.StatusForbidden)
		if sdkCalls.Load() != 1 {
			t.Fatal("sensitive tool reached the SDK")
		}
	})

	t.Run("credential inheritance stays at the same origin", func(t *testing.T) {
		item, err := toolsService.CreateTool(ctx, tool.CreateToolRequest{
			WorkspaceID: a, CreatedBy: owner.ID, Name: "credential_fixture", SourceType: "rest_api",
			EndpointConfig: `{"url":"http://127.0.0.1:8123/orders"}`,
			AuthConfig:     `{"header":"X-Fixture","value":"dummy-credential"}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := toolsPath + "/" + item.ID + "/versions"
		const newOrigin = `{"endpoint_config":"{\"url\":\"http://127.0.0.1:8124/orders\"}"}`
		request(http.MethodPost, path, newOrigin, ownerToken, a, http.StatusBadRequest)
		sameOrigin := `{"endpoint_config":"{\"url\":\"http://127.0.0.1:8123/another\"}"}`
		var kept domain.ToolVersion
		if err := json.Unmarshal(request(http.MethodPost, path, sameOrigin, ownerToken, a, http.StatusCreated), &kept); err != nil ||
			kept.Version != 2 || !kept.HasAuth {
			t.Fatalf("same-origin draft keeps stored authorization: %+v (%v)", kept, err)
		}
		const cleared = `{"endpoint_config":"{\"url\":\"http://127.0.0.1:8124/orders\"}","auth_config":"{}"}`
		var redirected domain.ToolVersion
		if err := json.Unmarshal(request(http.MethodPost, path, cleared, ownerToken, a, http.StatusCreated), &redirected); err != nil ||
			redirected.Version != 3 || redirected.HasAuth {
			t.Fatalf("new origin can explicitly clear auth: %+v (%v)", redirected, err)
		}
	})

	t.Run("real crossborder HTTP trial", func(t *testing.T) {
		endpoint := os.Getenv("ASTERFLOW_TEST_CROSSBORDER_TOOL_URL")
		if endpoint == "" {
			t.Skip("isolated Crossborder fixture is not running")
		}
		cfg, err := json.Marshal(map[string]string{"url": endpoint})
		if err != nil {
			t.Fatal(err)
		}
		item, err := toolsService.CreateTool(ctx, tool.CreateToolRequest{
			WorkspaceID: a, CreatedBy: owner.ID, Name: "crossborder_fixture", SourceType: "rest_api",
			EndpointConfig: string(cfg), SchemaJSON: querySchema,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := toolsPath + "/" + item.ID + "/test"
		const order = `{"input":{"order_id":"TTS-20260801-1001"}}`
		var run domain.ToolTestRun
		if err := json.Unmarshal(request(http.MethodPost, path, order, ownerToken, a, http.StatusOK), &run); err != nil ||
			run.Status != "success" || !strings.Contains(run.Output, "SKU-BLACK-M-01") || run.ToolVersionID == "" {
			t.Fatalf("unexpected order trial response: %+v (%v)", run, err)
		}
		last, err := store.GetToolLastSuccessfulTestRunForVersion(ctx, run.ToolVersionID)
		if err != nil || last.ID != run.ID {
			t.Fatalf("REST trial missing from PostgreSQL: %+v (%v)", last, err)
		}
	})
	t.Run("publish specific tool version", func(t *testing.T) {
		publishPath := func(itemID, versionID string) string {
			return toolsPath + "/" + itemID + "/versions/" + versionID + "/publish"
		}
		v1Path := publishPath(trialID, trialVersionID)

		request(http.MethodPost, v1Path, "", viewerToken, a, http.StatusForbidden)
		request(http.MethodPost, v1Path, "", ownerToken, b, http.StatusNotFound)
		request(http.MethodPost, publishPath(toolID, trialVersionID), "", ownerToken, a, http.StatusNotFound)

		v2, err := store.GetToolCurrentVersion(ctx, trialID)
		if err != nil || v2.Version != 2 {
			t.Fatalf("expected two versions from the trial fixture: %v", err)
		}
		payload := request(http.MethodPost, publishPath(trialID, v2.ID), "", ownerToken, a, http.StatusBadRequest)
		if !strings.Contains(string(payload), tool.ErrNoSuccessfulTestRun.Error()) {
			t.Fatalf("v2 cannot use a successful v1 run: %s", payload)
		}
		if _, err := pool.Exec(ctx, "UPDATE tool_test_runs SET created_at = now() - interval '8 days' WHERE tool_version_id::text = $1 AND status = 'success'", trialVersionID); err != nil {
			t.Fatal(err)
		}
		payload = request(http.MethodPost, v1Path, "", ownerToken, a, http.StatusBadRequest)
		if !strings.Contains(string(payload), tool.ErrToolTestRunExpired.Error()) {
			t.Fatalf("stale test unexpectedly eligible: %s", payload)
		}
		if _, err := toolsService.RecordTestRun(ctx, a, trialID, trialVersionID, "{}", "fixture", 1, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := toolsService.RecordTestRun(ctx, a, trialID, trialVersionID, "{}", "", 1, errors.New("fixture failed")); err != nil {
			t.Fatal(err)
		}

		type reply struct {
			status int
			err    error
		}
		start := make(chan struct{})
		replies := make(chan reply, 2)
		for range 2 {
			go func() {
				<-start
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+v1Path, nil)
				if err != nil {
					replies <- reply{err: err}
					return
				}
				req.Header.Set("Authorization", "Bearer "+ownerToken)
				req.Header.Set("X-Workspace-ID", a)
				res, err := server.Client().Do(req)
				if err != nil {
					replies <- reply{err: err}
					return
				}
				defer res.Body.Close()
				replies <- reply{status: res.StatusCode}
			}()
		}
		close(start)

		statuses := map[int]int{}
		for range 2 {
			result := <-replies
			if result.err != nil {
				t.Fatal(result.err)
			}
			statuses[result.status]++
		}
		if statuses[http.StatusOK] != 1 || statuses[http.StatusConflict] != 1 {
			t.Fatalf("want one publish and one conflict, got %v", statuses)
		}

		published, err := store.GetToolVersion(ctx, trialVersionID)
		if err != nil || published.Status != "published" {
			t.Fatalf("published state not returned from the database: %+v (%v)", published, err)
		}
		stillDraft, err := store.GetToolVersion(ctx, v2.ID)
		if err != nil || stillDraft.Status != "draft" {
			t.Fatalf("publishing v1 changed other version: %+v (%v)", stillDraft, err)
		}
	})

	t.Run("build Eino tool from the pinned version", func(t *testing.T) {
		var calls atomic.Int32
		versionsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"version_path": r.URL.Path})
		}))
		defer versionsServer.Close()
		configFor := func(path string) string {
			cfg, err := json.Marshal(map[string]string{"url": versionsServer.URL + path})
			if err != nil {
				t.Fatal(err)
			}
			return string(cfg)
		}
		item, err := toolsService.CreateTool(ctx, tool.CreateToolRequest{
			WorkspaceID: a, CreatedBy: owner.ID, Name: "pinned_order", SourceType: "rest_api",
			EndpointConfig: configFor("/v1"), SchemaJSON: querySchema,
		})
		if err != nil {
			t.Fatal(err)
		}
		v1, err := store.GetToolCurrentVersion(ctx, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		v2, err := toolsService.CreateToolVersion(ctx, a, item.ID, tool.CreateToolVersionRequest{
			EndpointConfig: configFor("/v2"),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{v1.ID, v2.ID} {
			if _, err := toolsService.RecordTestRun(ctx, a, item.ID, id, "{}", "fixture", 1, nil); err != nil {
				t.Fatal(err)
			}
			if err := toolsService.PublishToolVersion(ctx, a, item.ID, id); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := registry.BuildByVersion(ctx, a, v1.ID); err == nil {
			t.Fatal("a missing version loader was accepted")
		}
		registry.ConfigureVersionLoader(toolsService)
		if _, _, err := registry.BuildByVersion(ctx, b, v1.ID); !errors.Is(err, tool.ErrToolNotFound) {
			t.Fatalf("cross-space tool config became available: %v", err)
		}
		if _, _, err := registry.BuildByVersion(ctx, a, uuid.NewString()); !errors.Is(err, tool.ErrToolNotFound) {
			t.Fatalf("unknown version unexpectedly fell back: %v", err)
		}

		for _, tc := range []struct{ id, path string }{{v1.ID, "/v1"}, {v2.ID, "/v2"}} {
			built, identity, err := registry.BuildByVersion(ctx, a, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if identity != (domain.ToolIdentity{ToolID: item.ID, VersionID: tc.id}) {
				t.Fatalf("wrong identity for pinned version: %+v", identity)
			}
			info, err := built.Info(ctx)
			if err != nil || info.Name != item.Name {
				t.Fatalf("unexpected info: %+v (%v)", info, err)
			}
			before := calls.Load()
			if _, err := built.InvokableRun(ctx, "{}"); err == nil || calls.Load() != before {
				t.Fatalf("invalid arguments reached the HTTP endpoint: %v", err)
			}
			out, err := built.InvokableRun(ctx, "{\"order_id\":\"fixture\"}")
			var got map[string]string
			if decodeErr := json.Unmarshal([]byte(out), &got); err != nil || decodeErr != nil || got["version_path"] != tc.path {
				t.Fatalf("version %s invoked %q, want %s (execution %v, decode %v)", tc.id, out, tc.path, err, decodeErr)
			}
		}
	})
}

type delayedToolVersionStore struct {
	tool.Store
	read    chan<- struct{}
	proceed <-chan struct{}
}

func (s delayedToolVersionStore) GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	v, err := s.Store.GetToolCurrentVersion(ctx, toolID)
	if err != nil {
		return nil, err
	}
	close(s.read)
	select {
	case <-s.proceed:
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
