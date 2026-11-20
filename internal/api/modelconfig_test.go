package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
)

func TestModelConfigHTTP(t *testing.T) {
	pool := newUserHTTPTestPool(t)
	ctx := context.Background()
	migration, err := os.ReadFile("../../migrations/000003_model_accounts_deployments.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	profileMigration, err := os.ReadFile("../../migrations/000004_model_profiles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(profileMigration)); err != nil {
		t.Fatal(err)
	}
	bindingMigration, err := os.ReadFile("../../migrations/000005_project_model_bindings.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(bindingMigration)); err != nil {
		t.Fatal(err)
	}
	identities := iam.NewService(iam.NewPostgresStore(pool), []byte("http-test-jwt-key-at-least-32-bytes"))
	owner, err := identities.CreateUser(ctx, "owner@model.test", "test-password123", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := identities.CreateUser(ctx, "viewer@model.test", "test-password123", "Viewer")
	if err != nil {
		t.Fatal(err)
	}
	a, err := identities.CreateWorkspaceForUser(ctx, owner.ID, "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := identities.CreateWorkspaceForUser(ctx, owner.ID, "B", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identities.UpsertWorkspaceMember(ctx, owner.ID, a.ID, viewer.ID, iam.WorkspaceRoleViewer); err != nil {
		t.Fatal(err)
	}
	ownerLogin, err := identities.Login(ctx, iam.LoginRequest{Email: owner.Email, Password: "test-password123"})
	if err != nil {
		t.Fatal(err)
	}
	viewerLogin, err := identities.Login(ctx, iam.LoginRequest{Email: viewer.Email, Password: "test-password123"})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := modelconfig.NewCipher([]byte("model-http-test-master"))
	if err != nil {
		t.Fatal(err)
	}
	store := modelconfig.NewPostgresStore(pool)
	models := modelconfig.NewService(store, cipher)
	server := httptest.NewServer(NewRouter(pool, identities, models, nil))
	defer server.Close()
	call := func(method, path, token, space, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if space != "" {
			req.Header.Set("X-Workspace-ID", space)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal("HTTP request failed")
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != want {
			t.Fatalf("HTTP status=%d want=%d", response.StatusCode, want)
		}
		if strings.Contains(string(data), "test-only-api-key") || strings.Contains(string(data), "api_key_ciphertext") {
			t.Fatal("credential leaked in response")
		}
		return data
	}
	call("GET", "/api/v1/model-accounts", "", a.ID, "", 401)
	call("GET", "/api/v1/model-accounts", ownerLogin.Token, "", "", 400)
	if string(call("GET", "/api/v1/model-accounts", ownerLogin.Token, a.ID, "", 200)) != "[]" {
		t.Fatal("empty array required")
	}
	body := `{"name":"main","kind":"openai-compatible","base_url":"https://example.invalid/v1","api_key":"test-only-api-key"}`
	raw := call("POST", "/api/v1/model-accounts", ownerLogin.Token, a.ID, body, 201)
	var account modelconfig.ProviderAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		t.Fatal(err)
	}
	if account.WorkspaceID != a.ID || account.CreatedBy != owner.ID || !account.HasAPIKey {
		t.Fatal("trusted context was not used")
	}
	call("POST", "/api/v1/model-accounts", ownerLogin.Token, a.ID, body, 409)
	call("GET", "/api/v1/model-accounts", viewerLogin.Token, a.ID, "", 200)
	call("POST", "/api/v1/model-accounts", viewerLogin.Token, a.ID, body, 403)
	path := "/api/v1/model-accounts/" + account.ID + "/api-key"
	call("PUT", path, viewerLogin.Token, a.ID, `{"api_key":"replacement"}`, 403)
	call("PUT", path, ownerLogin.Token, b.ID, `{"api_key":"replacement"}`, 404)
	raw = call("PUT", path, ownerLogin.Token, a.ID, `{"api_key":"replacement"}`, 204)
	if len(raw) != 0 {
		t.Fatal("204 must have empty body")
	}
	recordQuery := "SELECT api_key_ciphertext FROM providers WHERE id=$1"
	var ciphertext []byte
	if err := pool.QueryRow(ctx, recordQuery, account.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	plain, err := cipher.Decrypt(ciphertext)
	if err != nil || plain != "replacement" {
		t.Fatal("rotation not persisted")
	}
	call("PUT", "/api/v1/model-accounts/invalid/api-key", ownerLogin.Token, a.ID, `{"api_key":"replacement"}`, 400)
	for _, invalid := range []string{"{", "{} {}", `{"workspace_id":"injected"}`, `{"created_by":"injected"}`, `{"name":"empty"}`} {
		call("POST", "/api/v1/model-accounts", ownerLogin.Token, a.ID, invalid, 400)
	}

	depBody, err := json.Marshal(map[string]any{
		"provider_account_id": account.ID, "name": "main-deployment", "model_name": "example-model",
		"input_price_per_million": 1.25, "output_price_per_million": 2.5, "cached_input_price_per_million": 0.1,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = call("POST", "/api/v1/model-deployments", ownerLogin.Token, a.ID, string(depBody), 201)
	var deployment modelconfig.ModelDeployment
	if err := json.Unmarshal(raw, &deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.WorkspaceID != a.ID || deployment.CreatedBy != owner.ID || deployment.ProviderAccountID != account.ID || deployment.TimeoutMS != 120000 || deployment.InputPricePerMillion != 1.25 {
		t.Fatal("deployment persisted fields incorrect")
	}
	call("POST", "/api/v1/model-deployments", ownerLogin.Token, a.ID, string(depBody), 409)
	call("POST", "/api/v1/model-deployments", viewerLogin.Token, a.ID, string(depBody), 403)
	call("POST", "/api/v1/model-deployments", ownerLogin.Token, b.ID, string(depBody), 404)
	call("GET", "/api/v1/model-deployments", "", a.ID, "", 401)
	call("GET", "/api/v1/model-deployments", ownerLogin.Token, "", "", 400)
	raw = call("GET", "/api/v1/model-deployments", viewerLogin.Token, a.ID, "", 200)
	var deployments []modelconfig.ModelDeployment
	if err := json.Unmarshal(raw, &deployments); err != nil || len(deployments) != 1 || deployments[0].ID != deployment.ID {
		t.Fatal("deployment list mismatch")
	}
	if string(call("GET", "/api/v1/model-deployments", ownerLogin.Token, b.ID, "", 200)) != "[]" {
		t.Fatal("deployment workspace isolation failed")
	}
	pricingPath := "/api/v1/model-deployments/" + deployment.ID + "/pricing"
	pricing := `{"input_price_per_million":3.5,"output_price_per_million":5,"cached_input_price_per_million":0.25}`
	call("PUT", pricingPath, viewerLogin.Token, a.ID, pricing, 403)
	call("PUT", pricingPath, ownerLogin.Token, b.ID, pricing, 404)
	raw = call("PUT", pricingPath, ownerLogin.Token, a.ID, pricing, 200)
	var updated modelconfig.ModelDeployment
	if err := json.Unmarshal(raw, &updated); err != nil || updated.InputPricePerMillion != 3.5 || updated.OutputPricePerMillion != 5 || updated.CachedInputPricePerMillion != 0.25 {
		t.Fatal("pricing response mismatch")
	}
	persisted, err := store.GetDeployment(ctx, deployment.ID)
	if err != nil || !persisted.UpdatedAt.Equal(updated.UpdatedAt) || persisted.InputPricePerMillion != 3.5 {
		t.Fatal("pricing not persisted")
	}
	call("PUT", pricingPath, ownerLogin.Token, a.ID, `{"input_price_per_million":-1,"output_price_per_million":0,"cached_input_price_per_million":0}`, 400)
	call("PUT", "/api/v1/model-deployments/invalid/pricing", ownerLogin.Token, a.ID, pricing, 400)
	call("POST", "/api/v1/model-deployments", ownerLogin.Token, a.ID, `{"created_by":"injected"}`, 400)
	call("POST", "/api/v1/model-deployments", ownerLogin.Token, a.ID, "{} {}", 400)

	// Profile HTTP contracts: real PG, trusted identity and workspace isolation.
	profileBody, _ := json.Marshal(map[string]any{
		"name": "support", "primary_deployment_id": deployment.ID,
		"fallback_deployment_ids": []string{}, "classification_max": "internal",
	})
	call("GET", "/api/v1/model-profiles", "", a.ID, "", 401)
	call("GET", "/api/v1/model-profiles", ownerLogin.Token, "", "", 400)
	if string(call("GET", "/api/v1/model-profiles", ownerLogin.Token, a.ID, "", 200)) != "[]" {
		t.Fatal("profiles must be []")
	}
	raw = call("POST", "/api/v1/model-profiles", ownerLogin.Token, a.ID, string(profileBody), 201)
	var created struct {
		Profile modelconfig.ModelProfile        `json:"profile"`
		Version modelconfig.ModelProfileVersion `json:"version"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if created.Profile.ID == "" || created.Profile.WorkspaceID != a.ID || created.Profile.CreatedBy != owner.ID ||
		created.Version.ProfileID != created.Profile.ID || created.Version.Version != 1 ||
		created.Version.PrimaryDeploymentID != deployment.ID || created.Version.CreatedBy != owner.ID ||
		created.Version.CreatedAt.IsZero() || created.Version.FallbackDeploymentIDs == nil {
		t.Fatal("profile trusted/persisted fields mismatch")
	}
	call("POST", "/api/v1/model-profiles", ownerLogin.Token, a.ID, string(profileBody), 409)
	call("POST", "/api/v1/model-profiles", viewerLogin.Token, a.ID, string(profileBody), 403)
	raw = call("GET", "/api/v1/model-profiles", viewerLogin.Token, a.ID, "", 200)
	var profiles []modelconfig.ModelProfile
	if err := json.Unmarshal(raw, &profiles); err != nil || len(profiles) != 1 || profiles[0].ID != created.Profile.ID {
		t.Fatal("profile list mismatch")
	}
	if string(call("GET", "/api/v1/model-profiles", ownerLogin.Token, b.ID, "", 200)) != "[]" {
		t.Fatal("profiles leaked across workspaces")
	}
	versionPath := "/api/v1/model-profiles/" + created.Profile.ID + "/versions"
	versionBody, _ := json.Marshal(map[string]any{"primary_deployment_id": deployment.ID, "fallback_deployment_ids": []string{deployment.ID}, "classification_max": "confidential"})
	call("GET", versionPath, "", a.ID, "", 401)
	call("GET", versionPath, ownerLogin.Token, "", "", 400)
	call("GET", versionPath, ownerLogin.Token, b.ID, "", 404)
	call("POST", versionPath, ownerLogin.Token, b.ID, string(versionBody), 404)
	call("POST", versionPath, viewerLogin.Token, a.ID, string(versionBody), 403)
	raw = call("POST", versionPath, ownerLogin.Token, a.ID, string(versionBody), 201)
	var v2 modelconfig.ModelProfileVersion
	if err := json.Unmarshal(raw, &v2); err != nil || v2.Version != 2 || v2.ProfileID != created.Profile.ID || v2.CreatedBy != owner.ID {
		t.Fatal("version writeback mismatch")
	}
	persistedVersion, err := store.GetProfileVersion(ctx, v2.ID)
	if err != nil {
		t.Fatalf("read persisted profile version: %v", err)
	}
	// pgx can return a Local timestamp while JSON with Z decodes as UTC.
	stored, response := *persistedVersion, v2
	stored.CreatedAt, response.CreatedAt = stored.CreatedAt.UTC(), response.CreatedAt.UTC()
	if !reflect.DeepEqual(stored, response) {
		t.Fatal("HTTP version differs from PG")
	}
	raw = call("GET", versionPath, viewerLogin.Token, a.ID, "", 200)
	var versions []modelconfig.ModelProfileVersion
	if err := json.Unmarshal(raw, &versions); err != nil || len(versions) != 2 || !reflect.DeepEqual(versions[0], created.Version) {
		t.Fatal("ordered history / immutable v1 mismatch")
	}
	call("GET", "/api/v1/model-profiles/invalid/versions", ownerLogin.Token, a.ID, "", 400)
	call("GET", "/api/v1/model-profiles/00000000-0000-0000-0000-000000000001/versions", ownerLogin.Token, a.ID, "", 404)
	for _, bad := range []string{"{} {}", `{"workspace_id":"injected"}`, `{"created_by":"injected"}`, `{"name":"","primary_deployment_id":"invalid"}`} {
		call("POST", "/api/v1/model-profiles", ownerLogin.Token, a.ID, bad, 400)
	}
	for _, bad := range []string{"{} {}", `{"version":999}`, `{"created_by":"injected"}`, `{"primary_deployment_id":"invalid"}`} {
		call("POST", versionPath, ownerLogin.Token, a.ID, bad, 400)
	}
	bAccount, err := models.CreateProviderAccount(ctx, modelconfig.CreateProviderAccountRequest{WorkspaceID: b.ID, Name: "other", Kind: "example", BaseURL: "https://example.invalid", APIKey: "fixture", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	bDeployment, err := models.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{WorkspaceID: b.ID, ProviderAccountID: bAccount.ID, Name: "other", ModelName: "example", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	badPrimary, _ := json.Marshal(map[string]any{"name": "foreign", "primary_deployment_id": bDeployment.ID})
	badFallback, _ := json.Marshal(map[string]any{"name": "foreign", "primary_deployment_id": deployment.ID, "fallback_deployment_ids": []string{bDeployment.ID}})
	for _, bad := range [][]byte{badPrimary, badFallback} {
		call("POST", "/api/v1/model-profiles", ownerLogin.Token, a.ID, string(bad), 404)
		// "name" is not a version field.
		var fields map[string]any
		_ = json.Unmarshal(bad, &fields)
		delete(fields, "name")
		payload, _ := json.Marshal(fields)
		call("POST", versionPath, ownerLogin.Token, a.ID, string(payload), 404)
	}
	raw = call("GET", versionPath, ownerLogin.Token, a.ID, "", 200)
	if err := json.Unmarshal(raw, &versions); err != nil || len(versions) != 2 {
		t.Fatal("rejected request persisted version")
	}

	const allVersionsPath = "/api/v1/model-profile-versions"
	const bindingsPath = "/api/v1/model-bindings"

	call("GET", allVersionsPath, "", a.ID, "", 401)
	call("GET", bindingsPath, ownerLogin.Token, "", "", 400)
	raw = call("GET", allVersionsPath, viewerLogin.Token, a.ID, "", 200)
	if err := json.Unmarshal(raw, &versions); err != nil || len(versions) != 2 ||
		versions[0].ID != created.Version.ID || versions[1].ID != v2.ID {
		t.Fatal("workspace version list mismatch")
	}
	if string(call("GET", allVersionsPath, ownerLogin.Token, b.ID, "", 200)) != "[]" {
		t.Fatal("workspace version list exposed another workspace")
	}
	raw = call("GET", bindingsPath, ownerLogin.Token, a.ID, "", 404)
	var notBound struct{ Code string }
	if err := json.Unmarshal(raw, &notBound); err != nil || notBound.Code != "model_binding_not_found" {
		t.Fatal("missing binding must be distinguishable from missing workspace")
	}

	putBody := func(env, version string, budget float64) string {
		data, err := json.Marshal(modelconfig.BindProjectRequest{
			Env: env, ModelProfileVersionID: version, MonthlyBudget: budget,
			RPMLimit: 60, TPMLimit: 20000,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	decodeBinding := func(raw []byte) modelconfig.ProjectBinding {
		t.Helper()
		var binding modelconfig.ProjectBinding
		if err := json.Unmarshal(raw, &binding); err != nil {
			t.Fatal(err)
		}
		return binding
	}

	devBody := putBody("dev", created.Version.ID, 100)
	call("PUT", bindingsPath, viewerLogin.Token, a.ID, devBody, 403)
	call("PUT", bindingsPath, ownerLogin.Token, b.ID, devBody, 404)
	call("PUT", bindingsPath, ownerLogin.Token, a.ID, putBody("", created.Version.ID, 0), 400)
	call("PUT", bindingsPath, ownerLogin.Token, a.ID, putBody("dev", "invalid", 0), 400)
	call("PUT", bindingsPath, ownerLogin.Token, a.ID, putBody("dev", created.Version.ID, -1), 400)
	call("PUT", bindingsPath, ownerLogin.Token, a.ID, `{"workspace_id":"injected"}`, 400)
	dev := decodeBinding(call("PUT", bindingsPath, ownerLogin.Token, a.ID, devBody, 200))
	if dev.WorkspaceID != a.ID || dev.Env != "dev" || dev.ModelProfileVersionID != created.Version.ID ||
		dev.MonthlyBudget != 100 || dev.RPMLimit != 60 || dev.TPMLimit != 20000 || dev.UpdatedAt.IsZero() {
		t.Fatal("saved binding fields mismatch")
	}
	prod := decodeBinding(call("PUT", bindingsPath, ownerLogin.Token, a.ID, putBody("prod", v2.ID, 0), 200))
	if prod.Env != "prod" || prod.ModelProfileVersionID != v2.ID {
		t.Fatal("prod must be configurable independently")
	}

	fallback, err := models.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{
		WorkspaceID: a.ID, ProviderAccountID: account.ID, Name: "unpriced-fallback",
		ModelName: "example-backup", CachedInputPricePerMillion: 0.25, CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	v3, err := models.CreateProfileVersion(ctx, created.Profile.ID, modelconfig.CreateProfileVersionRequest{
		WorkspaceID: a.ID, PrimaryDeploymentID: deployment.ID,
		FallbackDeploymentIDs: []string{fallback.ID}, CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	call("PUT", bindingsPath, ownerLogin.Token, a.ID, putBody("dev", v3.ID, 100), 400)
	got := decodeBinding(call("GET", bindingsPath, viewerLogin.Token, a.ID, "", 200))
	if got.ModelProfileVersionID != dev.ModelProfileVersionID || !got.UpdatedAt.Equal(dev.UpdatedAt) {
		t.Fatal("failed save modified the default dev binding")
	}
	call("PUT", "/api/v1/model-deployments/"+fallback.ID+"/pricing", ownerLogin.Token, a.ID, `{"input_price_per_million":0,"output_price_per_million":2,"cached_input_price_per_million":0.25}`, 200)
	got = decodeBinding(call("PUT", bindingsPath, ownerLogin.Token, a.ID, putBody("dev", v3.ID, 100), 200))
	if got.ModelProfileVersionID != v3.ID {
		t.Fatal("priced fallback should allow updating dev")
	}
	got = decodeBinding(call("GET", bindingsPath+"?env=prod", viewerLogin.Token, a.ID, "", 200))
	if got.ModelProfileVersionID != prod.ModelProfileVersionID || !got.UpdatedAt.Equal(prod.UpdatedAt) {
		t.Fatal("updating dev modified the prod binding")
	}
	call("GET", bindingsPath+"?env=dev", ownerLogin.Token, b.ID, "", 404)

	queryCredential := `{"name":"query-only-fixture","kind":"openai-compatible","base_url":"https://example.invalid/v1?api_key=synthetic-review-key","api_key":"test-only-api-key"}`
	invalidURL := call("POST", "/api/v1/model-accounts", ownerLogin.Token, a.ID, queryCredential, 400)
	viewerAccounts := call("GET", "/api/v1/model-accounts", viewerLogin.Token, a.ID, "", 200)
	if strings.Contains(string(invalidURL), "synthetic-review-key") ||
		strings.Contains(string(viewerAccounts), "query-only-fixture") {
		t.Fatal("credential-bearing URL was returned or persisted")
	}

	for _, body := range []string{"null", "{}", `{"output_price_per_million":2}`,
		`{"input_price_per_million":0,"output_price_per_million":null,"cached_input_price_per_million":0}`} {
		call("PUT", pricingPath, ownerLogin.Token, a.ID, body, 400)
	}
	unchanged, err := store.GetDeployment(ctx, deployment.ID)
	if err != nil || unchanged.InputPricePerMillion != 3.5 ||
		unchanged.OutputPricePerMillion != 5 || unchanged.CachedInputPricePerMillion != 0.25 {
		t.Fatal("invalid price replacement modified stored prices")
	}

	// A positive-budget binding depends on both its primary and fallback prices.
	clearPricing := `{"input_price_per_million":0,"output_price_per_million":0,"cached_input_price_per_million":0.25}`
	for _, path := range []string{pricingPath, "/api/v1/model-deployments/" + fallback.ID + "/pricing"} {
		raw := call("PUT", path, ownerLogin.Token, a.ID, clearPricing, 409)
		var problem struct{ Code string }
		if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "deployment_price_required" {
			t.Fatal("clearing either price path must return the dedicated conflict code")
		}
	}
	got = decodeBinding(call("GET", bindingsPath, ownerLogin.Token, a.ID, "", 200))
	if got.MonthlyBudget != 100 || got.ModelProfileVersionID != v3.ID {
		t.Fatal("rejected pricing request modified the active binding")
	}
	pricedFallback, err := store.GetDeployment(ctx, fallback.ID)
	if err != nil || pricedFallback.OutputPricePerMillion != 2 {
		t.Fatal("rejected price update modified persisted prices")
	}
}
