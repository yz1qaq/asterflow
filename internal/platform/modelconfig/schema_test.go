package modelconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAccountDeploymentSchema(t *testing.T) {
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("explicit test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("connect to explicit test database failed")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_model_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(c, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000001_init_iam.up.sql", "000002_workspace_rbac.up.sql", "000003_model_accounts_deployments.up.sql"} {
		data, err := os.ReadFile("../../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	cipher, err := NewCipher([]byte(strings.Repeat("x", 32)))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt("demo-key")
	if err != nil {
		t.Fatal(err)
	}
	accountID := uuid.New()
	insertAccount := "INSERT INTO providers(id,workspace_id,name,kind,base_url,api_key_ciphertext) VALUES($1,$2,$3,'example','https://example.invalid/v1',$4)"
	if _, err := conn.Exec(ctx, insertAccount, accountID, "space-a", "main", encrypted); err != nil {
		t.Fatal(err)
	}
	rejected := func(query, code string, args ...any) {
		t.Helper()
		_, err := conn.Exec(ctx, query, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Fatalf("expected SQLSTATE %s, got %v", code, err)
		}
	}
	rejected(insertAccount, "23505", uuid.New(), "space-a", "main", encrypted)
	if _, err := conn.Exec(ctx, insertAccount, uuid.New(), "space-b", "main", encrypted); err != nil {
		t.Fatal("same account name must be allowed in another workspace")
	}
	rejected(insertAccount, "23503", uuid.New(), "missing", "main", encrypted)
	var got []byte
	if err := conn.QueryRow(ctx, "SELECT api_key_ciphertext FROM providers WHERE id=$1", accountID).Scan(&got); err != nil || !bytes.Equal(got, encrypted) {
		t.Fatal("ciphertext was not stored as bytes")
	}
	insertDeployment := "INSERT INTO model_deployments(id,workspace_id,provider_account_id,name,model_name,input_price_per_million,timeout_ms,max_retries) VALUES($1,$2,$3,$4,'example-model',$5,$6,$7)"
	if _, err := conn.Exec(ctx, insertDeployment, uuid.New(), "space-a", accountID, "deployment", 1.25, 120000, 1); err != nil {
		t.Fatal(err)
	}
	rejected(insertDeployment, "23503", uuid.New(), "space-b", accountID, "cross-space", 0, 120000, 1)
	rejected(insertDeployment, "23514", uuid.New(), "space-a", accountID, "negative-price", -1, 120000, 1)
	rejected(insertDeployment, "23514", uuid.New(), "space-a", accountID, "invalid-timeout", 0, 0, 1)
	rejected(insertDeployment, "23514", uuid.New(), "space-a", accountID, "invalid-retries", 0, 120000, -1)
	data, err := os.ReadFile("../../../migrations/000003_model_accounts_deployments.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(data)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM workspaces").Scan(&count); err != nil || count != 2 {
		t.Fatal("down removed existing IAM data")
	}
	data, err = os.ReadFile("../../../migrations/000003_model_accounts_deployments.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(data)); err != nil {
		t.Fatal(err)
	}
}
func TestProviderRecordJSONHidesCiphertext(t *testing.T) {
	record := providerAccountRecord{ProviderAccount: ProviderAccount{ID: "account", WorkspaceID: "space-a", HasAPIKey: true}, APIKeyCiphertext: []byte("private-ciphertext")}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-ciphertext") || strings.Contains(string(raw), "APIKeyCiphertext") || strings.Contains(string(raw), "api_key_ciphertext") {
		t.Fatal("internal credential ciphertext leaked through JSON")
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data["has_api_key"] != true || data["id"] != "account" {
		t.Fatal("public account fields lost")
	}
}

func TestModelProfileSchema(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	apply := func(name string) {
		t.Helper()
		raw, err := os.ReadFile("../../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	apply("000004_model_profiles.up.sql")
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	account := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "main", Kind: "example", BaseURL: "https://example.invalid/v1", Status: "active"}}
	store := NewPostgresStore(pool)
	if err := store.CreateProviderAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	deployment := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: "space-a", ProviderAccountID: account.ID, Name: "primary", ModelName: "example", TimeoutMS: 120000, Status: "active"}
	if err := store.CreateDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	reject := func(query, code string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, query, args...)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != code {
			t.Fatalf("expected SQLSTATE %s, got %v", code, err)
		}
	}
	profileID := uuid.New()
	insertProfile := "INSERT INTO model_profiles(id,workspace_id,name) VALUES($1,$2,$3)"
	if _, err := pool.Exec(ctx, insertProfile, profileID, "space-a", "support"); err != nil {
		t.Fatal(err)
	}
	reject(insertProfile, "23505", uuid.New(), "space-a", "support")
	if _, err := pool.Exec(ctx, insertProfile, uuid.New(), "space-b", "support"); err != nil {
		t.Fatal("same name in another workspace must be allowed")
	}
	reject(insertProfile, "23503", uuid.New(), "missing", "support")
	secondProfile := uuid.New()
	if _, err := pool.Exec(ctx, insertProfile, secondProfile, "space-a", "another"); err != nil {
		t.Fatal(err)
	}
	versionID := uuid.New()
	insertVersion := "INSERT INTO model_profile_versions(id,profile_id,version,primary_deployment_id) VALUES($1,$2,$3,$4)"
	if _, err := pool.Exec(ctx, insertVersion, versionID, profileID, 1, deployment.ID); err != nil {
		t.Fatal(err)
	}
	var fallbacks []byte
	var classification string
	if err := pool.QueryRow(ctx, "SELECT fallback_deployment_ids,classification_max FROM model_profile_versions WHERE id=$1", versionID).Scan(&fallbacks, &classification); err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err := json.Unmarshal(fallbacks, &ids); err != nil || ids == nil || len(ids) != 0 || classification != "internal" {
		t.Fatal("default fallback array/classification incorrect")
	}
	reject(insertVersion, "23505", uuid.New(), profileID, 1, deployment.ID)
	if _, err := pool.Exec(ctx, insertVersion, uuid.New(), secondProfile, 1, deployment.ID); err != nil {
		t.Fatal("version sequence must be per-profile")
	}
	reject(insertVersion, "23514", uuid.New(), profileID, 0, deployment.ID)
	reject(insertVersion, "23503", uuid.New(), uuid.New(), 1, deployment.ID)
	reject(insertVersion, "23503", uuid.New(), profileID, 2, uuid.New())
	explicit := "INSERT INTO model_profile_versions(id,profile_id,version,primary_deployment_id,fallback_deployment_ids) VALUES($1,$2,$3,$4,$5)"
	reject(explicit, "23514", uuid.New(), profileID, 2, deployment.ID, []byte("{}"))
	reject(explicit, "23514", uuid.New(), profileID, 2, deployment.ID, []byte("null"))
	reject(explicit, "23502", uuid.New(), profileID, 2, deployment.ID, nil)
	idsJSON, err := json.Marshal([]string{deployment.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, explicit, uuid.New(), profileID, 2, deployment.ID, idsJSON); err != nil {
		t.Fatal(err)
	}
	reject("DELETE FROM model_deployments WHERE id=$1", "23503", deployment.ID)
	if _, err := pool.Exec(ctx, "DELETE FROM model_profiles WHERE id=$1", profileID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM model_profile_versions WHERE profile_id=$1", profileID).Scan(&count); err != nil || count != 0 {
		t.Fatal("profile delete did not cascade its versions")
	}
	apply("000004_model_profiles.down.sql")
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM model_deployments").Scan(&count); err != nil || count != 1 {
		t.Fatal("profile down damaged deployment data")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workspaces").Scan(&count); err != nil || count != 2 {
		t.Fatal("profile down damaged IAM data")
	}
	apply("000004_model_profiles.up.sql")
}
