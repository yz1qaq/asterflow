package modelconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresProviderAccounts(t *testing.T) {
	pool := newModelConfigTestPool(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	empty, err := store.ListProviderAccounts(ctx, "space-a")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty list must be non-nil")
	}
	cipher, err := NewCipher([]byte("test-only-master-key"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt("test-only-key-1")
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	record := &providerAccountRecord{
		ProviderAccount: ProviderAccount{
			ID:          strings.ReplaceAll(id.String(), "-", ""),
			WorkspaceID: "space-a", Name: "main", Kind: "openai_compatible",
			BaseURL: "https://example.invalid/v1", Status: "active", CreatedBy: "test-actor",
			CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
		},
		APIKeyCiphertext: encrypted,
	}
	if err := store.CreateProviderAccount(ctx, record); err != nil {
		t.Fatal(err)
	}
	if record.ID != id.String() || !record.HasAPIKey || record.CreatedAt.Before(time.Now().Add(-time.Minute)) || !record.CreatedAt.Equal(record.UpdatedAt) {
		t.Fatal("canonical persisted fields were not written back")
	}
	got, err := store.GetProviderAccount(ctx, record.ID)
	if err != nil || !reflect.DeepEqual(got, record) {
		t.Fatal("persisted account differs from created record")
	}
	got.Name = "caller-only"
	got.APIKeyCiphertext[0] ^= 1
	fresh, err := store.GetProviderAccount(ctx, record.ID)
	if err != nil || !reflect.DeepEqual(fresh, record) {
		t.Fatal("caller mutation affected persisted account")
	}
	duplicate := *record
	duplicate.ID = uuid.NewString()
	before := duplicate
	if err := store.CreateProviderAccount(ctx, &duplicate); !errors.Is(err, ErrProviderAccountNameExists) {
		t.Fatal("duplicate account name must be distinguishable")
	}
	if !reflect.DeepEqual(duplicate, before) {
		t.Fatal("failed create modified input")
	}
	collision := *record
	collision.Name = "another"
	var pgErr *pgconn.PgError
	err = store.CreateProviderAccount(ctx, &collision)
	if errors.Is(err, ErrProviderAccountNameExists) || !errors.As(err, &pgErr) || pgErr.ConstraintName != "providers_pkey" {
		t.Fatal("primary key conflict was mistaken for name conflict")
	}
	other := *record
	other.ID = uuid.NewString()
	other.WorkspaceID = "space-b"
	if err := store.CreateProviderAccount(ctx, &other); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListProviderAccounts(ctx, "space-a")
	if err != nil || len(listed) != 1 || listed[0].ID != record.ID {
		t.Fatal("list did not isolate workspace")
	}
	rotated, err := cipher.Encrypt("test-only-key-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProviderAPIKey(ctx, record.ID, rotated); err != nil {
		t.Fatal(err)
	}
	fresh, err = store.GetProviderAccount(ctx, record.ID)
	if err != nil || !bytes.Equal(fresh.APIKeyCiphertext, rotated) || fresh.UpdatedAt.Before(record.UpdatedAt) {
		t.Fatal("key rotation was not persisted")
	}
	plain, err := cipher.Decrypt(fresh.APIKeyCiphertext)
	if err != nil || plain != "test-only-key-2" {
		t.Fatal("rotated ciphertext cannot be decrypted")
	}
	if _, err := store.GetProviderAccount(ctx, uuid.NewString()); !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatal("missing lookup must be distinguishable")
	}
	if err := store.UpdateProviderAPIKey(ctx, uuid.NewString(), rotated); !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatal("missing rotation must be distinguishable")
	}
	if _, err := store.GetProviderAccount(ctx, "invalid"); err == nil {
		t.Fatal("invalid UUID accepted")
	}
	if err := store.UpdateProviderAPIKey(ctx, "invalid", rotated); err == nil {
		t.Fatal("invalid rotation UUID accepted")
	}
	if err := store.CreateProviderAccount(ctx, nil); err == nil {
		t.Fatal("nil account accepted")
	}
	invalid := *record
	invalid.ID = "invalid"
	if err := store.CreateProviderAccount(ctx, &invalid); err == nil {
		t.Fatal("invalid create UUID accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ListProviderAccounts(canceled, "space-a"); !errors.Is(err, context.Canceled) {
		t.Fatal("list lost cancellation cause")
	}
	if err := store.UpdateProviderAPIKey(canceled, record.ID, rotated); !errors.Is(err, context.Canceled) {
		t.Fatal("rotation lost cancellation cause")
	}
}

func newModelConfigTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ASTERFLOW_TEST_DATABASE_URL for isolated PostgreSQL modelconfig tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal("cannot connect to modelconfig test PostgreSQL; check explicit test URL")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	schema := "asterflow_model_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated modelconfig schema: %v", err)
		}
	})
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"000001_init_iam.up.sql", "000002_workspace_rbac.up.sql", "000003_model_accounts_deployments.up.sql"} {
		migration, err := os.ReadFile("../../../migrations/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("initialize isolated modelconfig schema: %v", err)
		}
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid modelconfig test PostgreSQL URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create modelconfig test pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("ping modelconfig test pool")
	}
	return pool
}

func TestPostgresDeployments(t *testing.T) {
	pool := newModelConfigTestPool(t)
	s := NewPostgresStore(pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	a := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "a", Kind: "example", BaseURL: "https://example.invalid", Status: "active"}}
	if err := s.CreateProviderAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	empty, err := s.ListDeployments(ctx, "space-a")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty deployment list")
	}
	d := &ModelDeployment{
		ID: uuid.NewString(), WorkspaceID: "space-a", ProviderAccountID: a.ID,
		Name: "primary", ModelName: "example-model", Region: "test", TimeoutMS: 120000, MaxRetries: 1,
		InputPricePerMillion: 0.1, OutputPricePerMillion: 2.5, CachedInputPricePerMillion: 0.025,
		Status: "active", CreatedBy: "test-actor",
	}
	if err := s.CreateDeployment(ctx, d); err != nil {
		t.Fatal(err)
	}
	if d.CreatedAt.IsZero() || !d.CreatedAt.Equal(d.UpdatedAt) {
		t.Fatal("timestamps not written back")
	}
	got, err := s.GetDeployment(ctx, d.ID)
	if err != nil || !reflect.DeepEqual(got, d) {
		t.Fatal("deployment roundtrip mismatch")
	}
	got.Name = "caller-only"
	list, err := s.ListDeployments(ctx, "space-a")
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], d) {
		t.Fatal("list mismatch")
	}
	if list, err := s.ListDeployments(ctx, "space-b"); err != nil || len(list) != 0 {
		t.Fatal("workspace leak")
	}
	duplicate := *d
	duplicate.ID = uuid.NewString()
	before := duplicate
	if err := s.CreateDeployment(ctx, &duplicate); !errors.Is(err, ErrDeploymentNameExists) {
		t.Fatal("name conflict not mapped")
	}
	if !reflect.DeepEqual(duplicate, before) {
		t.Fatal("failed create mutated input")
	}
	collision := *d
	collision.Name = "collision"
	err = s.CreateDeployment(ctx, &collision)
	var pgErr *pgconn.PgError
	if errors.Is(err, ErrDeploymentNameExists) || !errors.As(err, &pgErr) || pgErr.ConstraintName != "model_deployments_pkey" {
		t.Fatal("primary key conflict not preserved")
	}
	cross := *d
	cross.ID = uuid.NewString()
	cross.WorkspaceID = "space-b"
	err = s.CreateDeployment(ctx, &cross)
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatal("cross-workspace account accepted")
	}
	if err := s.UpdateDeploymentPricing(ctx, d.ID, 1.25, 3.5, 0); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetDeployment(ctx, d.ID)
	if err != nil || got.InputPricePerMillion != 1.25 || got.OutputPricePerMillion != 3.5 || got.CachedInputPricePerMillion != 0 || got.UpdatedAt.Before(d.UpdatedAt) {
		t.Fatal("pricing update mismatch")
	}
	missing := uuid.NewString()
	if _, err := s.GetDeployment(ctx, missing); !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatal("missing lookup")
	}
	if err := s.UpdateDeploymentPricing(ctx, missing, 0, 0, 0); !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatal("missing pricing update")
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := s.UpdateDeploymentPricing(ctx, d.ID, bad, 0, 0); err == nil {
			t.Fatal("invalid input price accepted")
		}
		if err := s.UpdateDeploymentPricing(ctx, d.ID, 0, bad, 0); err == nil {
			t.Fatal("invalid output price accepted")
		}
		if err := s.UpdateDeploymentPricing(ctx, d.ID, 0, 0, bad); err == nil {
			t.Fatal("invalid cached price accepted")
		}
	}
	for _, timeout := range []int{0, -1, math.MaxInt32 + 1} {
		invalid := *d
		invalid.ID = uuid.NewString()
		invalid.TimeoutMS = timeout
		if err := s.CreateDeployment(ctx, &invalid); err == nil {
			t.Fatal("invalid timeout accepted")
		}
	}
	for _, retry := range []int{-1, math.MaxInt32 + 1} {
		invalid := *d
		invalid.ID = uuid.NewString()
		invalid.MaxRetries = retry
		if err := s.CreateDeployment(ctx, &invalid); err == nil {
			t.Fatal("invalid retry count accepted")
		}
	}
	if err := s.CreateDeployment(ctx, nil); err == nil {
		t.Fatal("nil deployment accepted")
	}
	if _, err := s.GetDeployment(ctx, "invalid"); err == nil {
		t.Fatal("invalid deployment ID accepted")
	}
	if err := s.UpdateDeploymentPricing(ctx, "invalid", 0, 0, 0); err == nil {
		t.Fatal("invalid pricing ID accepted")
	}
	invalid := *d
	invalid.ID = "invalid"
	if err := s.CreateDeployment(ctx, &invalid); err == nil {
		t.Fatal("invalid create ID accepted")
	}
	invalid = *d
	invalid.ProviderAccountID = "invalid"
	if err := s.CreateDeployment(ctx, &invalid); err == nil {
		t.Fatal("invalid account ID accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ListDeployments(canceled, "space-a"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if err := s.UpdateDeploymentPricing(canceled, d.ID, 0, 0, 0); !errors.Is(err, context.Canceled) {
		t.Fatal("pricing cancellation lost")
	}
	var row pgstore.ModelDeployment
	if _, err := deploymentFromRow(row); err == nil {
		t.Fatal("NULL numeric accepted")
	}
	var nan pgtype.Numeric
	if err := nan.Scan("NaN"); err != nil {
		t.Fatal(err)
	}
	row.InputPricePerMillion = nan
	if _, err := deploymentFromRow(row); err == nil {
		t.Fatal("NaN stored price accepted")
	}
}

func TestPostgresProfileQueries(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	raw, err := os.ReadFile("../../../migrations/000004_model_profiles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	account := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "main", Kind: "example", BaseURL: "https://example.invalid", Status: "active"}}
	if err := store.CreateProviderAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	deployment := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: "space-a", ProviderAccountID: account.ID, Name: "main", ModelName: "example", TimeoutMS: 120000, Status: "active"}
	if err := store.CreateDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	q := pgstore.New(pool)
	profileID := uuid.New()
	profile, err := q.CreateProfile(ctx, pgstore.CreateProfileParams{ID: profileID, WorkspaceID: "space-a", Name: "support", Description: "support profile", CreatedBy: "test"})
	if err != nil || profile.ID != profileID || profile.CreatedAt.IsZero() {
		t.Fatal("profile insert/return mismatch")
	}
	if _, err := q.GetProfile(ctx, profileID); err != nil {
		t.Fatal(err)
	}
	list, err := q.ListProfiles(ctx, "space-a")
	if err != nil || len(list) != 1 {
		t.Fatal("profile list mismatch")
	}
	other, err := q.ListProfiles(ctx, "space-b")
	if err != nil || other == nil || len(other) != 0 {
		t.Fatal("workspace isolation failed")
	}
	next, err := q.NextModelProfileVersion(ctx, profileID)
	if err != nil || next != 1 {
		t.Fatal("first version number incorrect")
	}
	versionID := uuid.New()
	version, err := q.CreateProfileVersion(ctx, pgstore.CreateProfileVersionParams{
		ID: versionID, ProfileID: profileID, Version: int32(next),
		PrimaryDeploymentID: uuid.MustParse(deployment.ID), FallbackDeploymentIds: []byte("[]"),
		ClassificationMax: "internal", CreatedBy: "test",
	})
	if err != nil || version.ID != versionID || version.ProfileID != profileID || version.CreatedAt.IsZero() {
		t.Fatal("version insert/return mismatch")
	}
	if _, err := q.GetProfileVersion(ctx, versionID); err != nil {
		t.Fatal(err)
	}
	versions, err := q.ListProfileVersions(ctx, profileID)
	if err != nil || len(versions) != 1 || versions[0].Version != 1 {
		t.Fatal("versions list mismatch")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qt := q.WithTx(tx)
	locked, err := qt.LockModelProfile(ctx, profileID)
	if err != nil || locked.WorkspaceID != "space-a" {
		t.Fatal("profile row lock mismatch")
	}
	next, err = qt.NextModelProfileVersion(ctx, profileID)
	if err != nil || next != 2 {
		t.Fatal("next number after first version incorrect")
	}
	if _, err := qt.CreateProfileVersion(ctx, pgstore.CreateProfileVersionParams{
		ID: uuid.New(), ProfileID: profileID, Version: int32(next),
		PrimaryDeploymentID: uuid.MustParse(deployment.ID), FallbackDeploymentIds: []byte("[]"),
		ClassificationMax: "internal", CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	next, err = q.NextModelProfileVersion(ctx, profileID)
	if err != nil || next != 3 {
		t.Fatal("committed version not reflected")
	}
	versions, err = q.ListProfileVersions(ctx, profileID)
	if err != nil || len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatal("version order incorrect")
	}
	_, err = q.GetProfileVersion(ctx, uuid.New())
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("missing version must preserve not-found cause")
	}
	_, err = q.CreateProfile(ctx, pgstore.CreateProfileParams{ID: uuid.New(), WorkspaceID: "space-a", Name: "support"})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "model_profiles_workspace_id_name_key" {
		t.Fatal("profile name constraint mismatch")
	}
}

func TestPostgresProfileAtomicCreate(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	data, err := os.ReadFile("../../../migrations/000004_model_profiles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES('space-a','A')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	q := pgstore.New(pool)
	account := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "main", Kind: "example", BaseURL: "https://example.invalid", Status: "active"}}
	if err := store.CreateProviderAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	deployment := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: "space-a", ProviderAccountID: account.ID, Name: "main", ModelName: "example", TimeoutMS: 120000, Status: "active"}
	if err := store.CreateDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	pair := func(name string) (*ModelProfile, *ModelProfileVersion) {
		id := uuid.New()
		return &ModelProfile{ID: strings.ReplaceAll(id.String(), "-", ""), WorkspaceID: "space-a", Name: name, CreatedBy: "actor", CreatedAt: time.Unix(1, 0)},
			&ModelProfileVersion{ID: uuid.NewString(), ProfileID: id.String(), Version: 1, PrimaryDeploymentID: deployment.ID, ClassificationMax: "internal", CreatedBy: "actor", CreatedAt: time.Unix(1, 0)}
	}
	p, v := pair("support")
	if err := store.CreateProfileWithVersion(ctx, p, v); err != nil {
		t.Fatal(err)
	}
	if len(p.ID) != 36 || p.CreatedAt.Before(time.Now().Add(-time.Minute)) || v.CreatedAt.Before(time.Now().Add(-time.Minute)) || v.ProfileID != p.ID || v.FallbackDeploymentIDs == nil || len(v.FallbackDeploymentIDs) != 0 {
		t.Fatal("committed database values not written back")
	}
	pRow, err := q.GetProfile(ctx, uuid.MustParse(p.ID))
	if err != nil || !reflect.DeepEqual(profileFromRow(pRow), p) {
		t.Fatal("profile roundtrip mismatch")
	}
	vRow, err := q.GetProfileVersion(ctx, uuid.MustParse(v.ID))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := profileVersionFromRow(vRow)
	if err != nil || !reflect.DeepEqual(persisted, v) || string(vRow.FallbackDeploymentIds) != "[]" {
		t.Fatal("version roundtrip mismatch")
	}
	badP, badV := pair("rollback")
	badV.PrimaryDeploymentID = uuid.NewString()
	beforeP, beforeV := *badP, *badV
	err = store.CreateProfileWithVersion(ctx, badP, badV)
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "23503" {
		t.Fatal("second insert must preserve FK failure")
	}
	if !reflect.DeepEqual(beforeP, *badP) || !reflect.DeepEqual(beforeV, *badV) {
		t.Fatal("failed transaction modified caller objects")
	}
	if _, err := q.GetProfile(ctx, uuid.MustParse(badP.ID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("failed version insert left an orphan profile")
	}
	duplicateP, duplicateV := pair("support")
	beforeP, beforeV = *duplicateP, *duplicateV
	if err := store.CreateProfileWithVersion(ctx, duplicateP, duplicateV); !errors.Is(err, ErrProfileNameExists) {
		t.Fatal("duplicate profile name not mapped")
	}
	if !reflect.DeepEqual(beforeP, *duplicateP) || !reflect.DeepEqual(beforeV, *duplicateV) {
		t.Fatal("name failure mutated inputs")
	}
	collisionP, collisionV := pair("version-id-collision")
	collisionV.ID = v.ID
	err = store.CreateProfileWithVersion(ctx, collisionP, collisionV)
	if errors.Is(err, ErrProfileNameExists) || !errors.As(err, &pe) || pe.ConstraintName != "model_profile_versions_pkey" {
		t.Fatal("version primary key error lost")
	}
	if _, err := q.GetProfile(ctx, uuid.MustParse(collisionP.ID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("version PK failure left an orphan profile")
	}
	for _, field := range []string{"profile-id", "version-id", "parent-id", "parent-mismatch", "primary-id", "number"} {
		invalidP, invalidV := pair("invalid-" + field)
		switch field {
		case "profile-id":
			invalidP.ID = "invalid"
		case "version-id":
			invalidV.ID = "invalid"
		case "parent-id":
			invalidV.ProfileID = "invalid"
		case "parent-mismatch":
			invalidV.ProfileID = uuid.NewString()
		case "primary-id":
			invalidV.PrimaryDeploymentID = "invalid"
		case "number":
			invalidV.Version = 2
		}
		if err := store.CreateProfileWithVersion(ctx, invalidP, invalidV); err == nil {
			t.Fatal("invalid initial profile/version accepted")
		}
	}
	if err := store.CreateProfileWithVersion(ctx, nil, v); err == nil {
		t.Fatal("nil profile accepted")
	}
	if err := store.CreateProfileWithVersion(ctx, p, nil); err == nil {
		t.Fatal("nil version accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	canceledP, canceledV := pair("canceled")
	if err := store.CreateProfileWithVersion(canceled, canceledP, canceledV); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation cause lost")
	}
	if _, err := q.GetProfile(ctx, uuid.MustParse(canceledP.ID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("canceled creation left a profile")
	}
	for _, raw := range []string{"null", "{}", "[1]", "invalid"} {
		if _, err := profileVersionFromRow(pgstore.ModelProfileVersion{FallbackDeploymentIds: []byte(raw)}); err == nil {
			t.Fatal("invalid stored fallback list accepted")
		}
	}
}

func TestPostgresProfileReadAppend(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	migration, err := os.ReadFile("../../../migrations/000004_model_profiles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	s := NewPostgresStore(pool)
	a := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "main", Kind: "example", BaseURL: "https://example.invalid", Status: "active"}}
	if err = s.CreateProviderAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	d := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: "space-a", ProviderAccountID: a.ID, Name: "main", ModelName: "example", TimeoutMS: 120000, Status: "active"}
	if err = s.CreateDeployment(ctx, d); err != nil {
		t.Fatal(err)
	}
	p := &ModelProfile{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "support"}
	first := &ModelProfileVersion{ID: uuid.NewString(), ProfileID: p.ID, Version: 1, PrimaryDeploymentID: d.ID, ClassificationMax: "internal"}
	if err = s.CreateProfileWithVersion(ctx, p, first); err != nil {
		t.Fatal(err)
	}
	next := func() *ModelProfileVersion {
		return &ModelProfileVersion{ID: uuid.NewString(), ProfileID: p.ID, Version: 999, PrimaryDeploymentID: d.ID, ClassificationMax: "confidential"}
	}
	got, err := s.GetProfile(ctx, p.ID)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("profile read: %v", err)
	}
	profiles, err := s.ListProfiles(ctx, "space-a")
	if err != nil || len(profiles) != 1 || !reflect.DeepEqual(profiles[0], p) {
		t.Fatalf("profile list: %v", err)
	}
	empty, err := s.ListProfiles(ctx, "space-b")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty space: %v", err)
	}
	initial, err := s.GetProfileVersion(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(initial, first) {
		t.Fatalf("version read: %v", err)
	}
	if _, err = s.GetProfile(ctx, uuid.NewString()); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("missing profile: %v", err)
	}
	if _, err = s.GetProfileVersion(ctx, uuid.NewString()); !errors.Is(err, ErrProfileVersionNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	noVersions, err := s.ListProfileVersions(ctx, uuid.NewString())
	if err != nil || noVersions == nil || len(noVersions) != 0 {
		t.Fatalf("empty versions: %v", err)
	}
	for name, err := range map[string]error{
		"profile":  func() error { _, e := s.GetProfile(ctx, "invalid"); return e }(),
		"version":  func() error { _, e := s.GetProfileVersion(ctx, "invalid"); return e }(),
		"versions": func() error { _, e := s.ListProfileVersions(ctx, "invalid"); return e }(),
	} {
		if err == nil {
			t.Fatal("invalid UUID accepted: " + name)
		}
	}
	second := next()
	if err = s.CreateProfileVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 || second.FallbackDeploymentIDs == nil || second.CreatedAt.IsZero() {
		t.Fatal("store did not assign committed version")
	}
	fresh, err := s.GetProfileVersion(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(fresh, first) {
		t.Fatal("old version changed")
	}
	bad := next()
	bad.PrimaryDeploymentID = uuid.NewString()
	before := *bad
	if err = s.CreateProfileVersion(ctx, bad); err == nil || !reflect.DeepEqual(*bad, before) {
		t.Fatal("failed append changed input")
	}
	third := next()
	if err = s.CreateProfileVersion(ctx, third); err != nil || third.Version != 3 {
		t.Fatalf("rollback consumed number: %v", err)
	}
	missing := next()
	missing.ProfileID = uuid.NewString()
	if err = s.CreateProfileVersion(ctx, missing); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("append missing parent: %v", err)
	}
	if err = s.CreateProfileVersion(ctx, nil); err == nil {
		t.Fatal("nil accepted")
	}

	t.Run("concurrent", func(t *testing.T) {
		const count = 6
		start := make(chan struct{})
		results := make(chan *ModelProfileVersion, count)
		errs := make(chan error, count)
		var wg sync.WaitGroup
		for i := 0; i < count; i++ {
			v := next()
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if e := s.CreateProfileVersion(ctx, v); e != nil {
					errs <- e
				} else {
					results <- v
				}
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			t.Error(e)
		}
		seen := map[int]bool{}
		for v := range results {
			if v.Version < 4 || v.Version > 9 || seen[v.Version] {
				t.Errorf("bad version %d", v.Version)
			}
			seen[v.Version] = true
		}
		if len(seen) != count {
			t.Fatal("not all appenders succeeded")
		}
		versions, e := s.ListProfileVersions(ctx, p.ID)
		if e != nil || len(versions) != 9 {
			t.Fatalf("version list: %v", e)
		}
		for i, v := range versions {
			if v.Version != i+1 {
				t.Fatal("not ordered")
			}
		}
	})

	t.Run("cancel while waiting for lock", func(t *testing.T) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		if _, e = pgstore.New(pool).WithTx(tx).LockModelProfile(ctx, uuid.MustParse(p.ID)); e != nil {
			t.Fatal(e)
		}
		cfg := pool.Config()
		marker := "asterflow_append_" + uuid.NewString()
		cfg.ConnConfig.RuntimeParams["application_name"] = marker
		waitingPool, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer waitingPool.Close()
		v := next()
		before := *v
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- NewPostgresStore(waitingPool).CreateProfileVersion(cancelCtx, v) }()
		deadline := time.Now().Add(5 * time.Second)
		blocked := false
		for time.Now().Before(deadline) {
			if e = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')", marker).Scan(&blocked); e != nil {
				t.Fatal(e)
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			cancel()
			_ = tx.Rollback(ctx)
			<-done
			t.Fatal("request did not reach lock wait")
		}
		cancel()
		select {
		case e = <-done:
			if !errors.Is(e, context.Canceled) {
				t.Fatalf("lost cancel: %v", e)
			}
		case <-time.After(5 * time.Second):
			_ = tx.Rollback(ctx)
			t.Fatal("cancel did not unblock")
		}
		if !reflect.DeepEqual(*v, before) {
			t.Fatal("canceled append changed input")
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		after := next()
		if e = s.CreateProfileVersion(ctx, after); e != nil || after.Version != 10 {
			t.Fatalf("lock/cancel left gap: %v", e)
		}
	})

	t.Run("overflow", func(t *testing.T) {
		_, e := pgstore.New(pool).CreateProfileVersion(ctx, pgstore.CreateProfileVersionParams{ID: uuid.New(), ProfileID: uuid.MustParse(p.ID), Version: math.MaxInt32, PrimaryDeploymentID: uuid.MustParse(d.ID), FallbackDeploymentIds: []byte("[]"), ClassificationMax: "internal"})
		if e != nil {
			t.Fatal(e)
		}
		v := next()
		before := *v
		if e = s.CreateProfileVersion(ctx, v); e == nil || !reflect.DeepEqual(*v, before) {
			t.Fatal("overflow accepted or mutated")
		}
		_, e = pgstore.New(pool).GetProfileVersion(ctx, uuid.MustParse(v.ID))
		if !errors.Is(e, pgx.ErrNoRows) {
			t.Fatal(fmt.Sprintf("overflow left row: %v", e))
		}
	})
}

func TestPostgresBindingQueries(t *testing.T) {
	ctx := context.Background()
	pool := newModelConfigTestPool(t)
	apply := func(file string) {
		t.Helper()
		data, err := os.ReadFile("../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	apply("000004_model_profiles.up.sql")
	apply("000005_project_model_bindings.up.sql")
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	createVersion := func(workspace string) (*ModelProfile, *ModelProfileVersion) {
		t.Helper()
		account := &providerAccountRecord{ProviderAccount: ProviderAccount{
			ID: uuid.NewString(), WorkspaceID: workspace, Name: "main", Kind: "example",
			BaseURL: "https://example.invalid", Status: "active",
		}}
		if err := store.CreateProviderAccount(ctx, account); err != nil {
			t.Fatal(err)
		}
		deployment := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: workspace,
			ProviderAccountID: account.ID, Name: "main", ModelName: "example",
			TimeoutMS: 120000, Status: "active",
		}
		if err := store.CreateDeployment(ctx, deployment); err != nil {
			t.Fatal(err)
		}
		profile := &ModelProfile{ID: uuid.NewString(), WorkspaceID: workspace, Name: "Support"}
		version := &ModelProfileVersion{ID: uuid.NewString(), ProfileID: profile.ID, Version: 1,
			PrimaryDeploymentID: deployment.ID, ClassificationMax: "internal"}
		if err := store.CreateProfileWithVersion(ctx, profile, version); err != nil {
			t.Fatal(err)
		}
		return profile, version
	}
	profile, first := createVersion("space-a")
	_, foreign := createVersion("space-b")
	second := *first
	second.ID = uuid.NewString()
	if err := store.CreateProfileVersion(ctx, &second); err != nil {
		t.Fatal(err)
	}
	q := pgstore.New(pool)
	versions, err := q.ListWorkspaceProfileVersions(ctx, "space-a")
	if err != nil || len(versions) != 2 || versions[0].ProfileID.String() != profile.ID ||
		versions[0].ID.String() != first.ID || versions[1].ID.String() != second.ID {
		t.Fatalf("workspace versions: %v", err)
	}
	var budget pgtype.Numeric
	if err := budget.Scan("12.75"); err != nil {
		t.Fatal(err)
	}
	params := pgstore.UpsertProjectBindingParams{WorkspaceID: "space-a", Env: "dev",
		ModelProfileVersionID: uuid.MustParse(first.ID), MonthlyBudget: budget, RpmLimit: 10, TpmLimit: 500}
	row, err := q.UpsertProjectBinding(ctx, params)
	if err != nil || row.ModelProfileVersionID.String() != first.ID || row.UpdatedAt.IsZero() {
		t.Fatalf("insert: %v", err)
	}
	params.Env = "prod"
	if _, err := q.UpsertProjectBinding(ctx, params); err != nil {
		t.Fatal(err)
	}
	params.Env = "dev"
	params.ModelProfileVersionID = uuid.MustParse(second.ID)
	params.RpmLimit = 20
	if _, err := q.UpsertProjectBinding(ctx, params); err != nil {
		t.Fatal(err)
	}
	for _, badID := range []uuid.UUID{uuid.MustParse(foreign.ID), uuid.New()} {
		bad := params
		bad.ModelProfileVersionID = badID
		if _, err := q.UpsertProjectBinding(ctx, bad); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("forbidden upsert: %v", err)
		}
	}
	for _, raw := range []string{"-1", "NaN", "Infinity"} {
		bad := params
		if err := bad.MonthlyBudget.Scan(raw); err != nil {
			t.Fatal(err)
		}
		_, err := q.UpsertProjectBinding(ctx, bad)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("invalid budget %q: %v", raw, err)
		}
	}
	dev, err := q.GetProjectBinding(ctx, pgstore.GetProjectBindingParams{WorkspaceID: "space-a", Env: "dev"})
	if err != nil || dev.ModelProfileVersionID.String() != second.ID || dev.RpmLimit != 20 || dev.TpmLimit != 500 {
		t.Fatalf("dev read: %v", err)
	}
	prod, err := q.GetProjectBinding(ctx, pgstore.GetProjectBindingParams{WorkspaceID: "space-a", Env: "prod"})
	if err != nil || prod.ModelProfileVersionID.String() != first.ID || prod.RpmLimit != 10 {
		t.Fatalf("prod read: %v", err)
	}
	maxLimit := int(math.MaxInt32)
	if _, err := store.GetProjectBinding(ctx, "space-b", "dev"); !errors.Is(err, ErrProjectBindingNotFound) {
		t.Fatalf("missing binding returned %v", err)
	}
	binding := &ProjectBinding{WorkspaceID: "space-a", Env: "stage",
		ModelProfileVersionID: strings.ReplaceAll(first.ID, "-", ""), MonthlyBudget: 42.75,
		RPMLimit: math.MaxInt32, TPMLimit: 0,
	}
	if err := store.UpsertProjectBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if binding.ModelProfileVersionID != first.ID || binding.UpdatedAt.IsZero() {
		t.Fatalf("save did not write back returned fields: %+v", *binding)
	}
	loaded, err := store.GetProjectBinding(ctx, binding.WorkspaceID, binding.Env)
	if err != nil || !reflect.DeepEqual(loaded, binding) {
		t.Fatalf("loaded binding differs from saved record: %v", err)
	}
	for _, badID := range []string{foreign.ID, uuid.NewString()} {
		rejected := *binding
		rejected.ModelProfileVersionID = badID
		before := rejected
		err := store.UpsertProjectBinding(ctx, &rejected)
		if !errors.Is(err, ErrProfileVersionNotFound) || rejected != before {
			t.Fatalf("wrong write error or input mutated: %v", err)
		}
	}
	for name, patch := range map[string]func(*ProjectBinding){
		"NaN":          func(b *ProjectBinding) { b.MonthlyBudget = math.NaN() },
		"Infinity":     func(b *ProjectBinding) { b.MonthlyBudget = math.Inf(1) },
		"Negative":     func(b *ProjectBinding) { b.MonthlyBudget = -1 },
		"RPM overflow": func(b *ProjectBinding) { b.RPMLimit = maxLimit + 1 },
		"TPM negative": func(b *ProjectBinding) { b.TPMLimit = -1 },
	} {
		rejected := *binding
		patch(&rejected)
		if err := store.UpsertProjectBinding(ctx, &rejected); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	malformed := *binding
	malformed.ModelProfileVersionID = "not-a-uuid"
	err = store.UpsertProjectBinding(ctx, &malformed)
	if err == nil || !strings.Contains(err.Error(), "parse profile version id") || errors.Is(err, ErrProfileVersionNotFound) {
		t.Fatalf("malformed UUID must be rejected at the parsing boundary: %v", err)
	}
	loaded, err = store.GetProjectBinding(ctx, binding.WorkspaceID, binding.Env)
	if err != nil || !reflect.DeepEqual(loaded, binding) {
		t.Fatalf("rejected write changed persisted binding: %v", err)
	}
	all, err := store.ListWorkspaceProfileVersions(ctx, "space-a")
	if err != nil || len(all) != 2 || !reflect.DeepEqual(all[0], first) || !reflect.DeepEqual(all[1], &second) {
		t.Fatalf("list workspace versions: %v", err)
	}
	empty, err := store.ListWorkspaceProfileVersions(ctx, "missing-space")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("expected nonnil empty version list: %v", err)
	}

	apply("000005_project_model_bindings.down.sql")
	if _, err := store.GetProfileVersion(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	apply("000005_project_model_bindings.up.sql")
	if _, err := q.GetProjectBinding(ctx, pgstore.GetProjectBindingParams{WorkspaceID: "space-a", Env: "dev"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unexpected surviving binding: %v", err)
	}
}

func TestPostgresWorkspaceModelWrite(t *testing.T) {
	ctx := context.Background()
	pool := newModelConfigTestPool(t)
	for _, name := range []string{"000004_model_profiles.up.sql", "000005_project_model_bindings.up.sql"} {
		text, err := os.ReadFile("../../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(text)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	account := &providerAccountRecord{ProviderAccount: ProviderAccount{
		ID: uuid.NewString(), WorkspaceID: "space-a", Name: "provider", Kind: "example",
		BaseURL: "https://example.invalid/v1", Status: "active",
	}}
	if err := store.CreateProviderAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	deployment := func(name string) string {
		t.Helper()
		d := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: "space-a", ProviderAccountID: account.ID,
			Name: name, ModelName: "example", TimeoutMS: 1000, Status: "active"}
		if err := store.CreateDeployment(ctx, d); err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	primary, fallback, unrelated := deployment("primary"), deployment("fallback"), deployment("unrelated")
	profile := &ModelProfile{ID: uuid.NewString(), WorkspaceID: "space-a", Name: "Support"}
	version := &ModelProfileVersion{ID: uuid.NewString(), ProfileID: profile.ID, Version: 1,
		PrimaryDeploymentID: primary, FallbackDeploymentIDs: []string{fallback}, ClassificationMax: "internal"}
	if err := store.CreateProfileWithVersion(ctx, profile, version); err != nil {
		t.Fatal(err)
	}
	check := func(reader interface {
		HasBudgetBindingForDeployment(context.Context, string, string) (bool, error)
	}, space, deployment string, want bool) {
		t.Helper()
		got, err := reader.HasBudgetBindingForDeployment(ctx, space, deployment)
		if err != nil || got != want {
			t.Fatalf("budget binding result %t, want %t: %v", got, want, err)
		}
	}
	binding := ProjectBinding{WorkspaceID: "space-a", Env: "dev", ModelProfileVersionID: version.ID}
	if err := store.UpsertProjectBinding(ctx, &binding); err != nil {
		t.Fatal(err)
	}
	check(store, "space-a", primary, false)
	stop := errors.New("rollback marker")
	write := func(returnErr error) error {
		return store.WithWorkspaceModelWrite(ctx, "space-a", func(w WorkspaceModelWriter) error {
			change := binding
			change.MonthlyBudget = 33
			if err := w.UpsertProjectBinding(ctx, &change); err != nil {
				return err
			}
			check(w, "space-a", primary, true)
			check(w, "space-a", fallback, true)
			check(w, "space-a", unrelated, false)
			check(w, "space-b", fallback, false)
			return returnErr
		})
	}
	if err := write(stop); !errors.Is(err, stop) {
		t.Fatalf("rollback must return the callback error: %v", err)
	}
	check(store, "space-a", fallback, false)
	if err := write(nil); err != nil {
		t.Fatal(err)
	}
	check(store, "space-a", fallback, true)

	if err := store.WithWorkspaceModelWrite(ctx, "space-a", func(w WorkspaceModelWriter) error {
		waitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		sameSpaceRan := false
		err := store.WithWorkspaceModelWrite(waitCtx, "space-a", func(WorkspaceModelWriter) error {
			sameSpaceRan = true
			return nil
		})
		if sameSpaceRan || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("second writer must time out waiting for the same workspace: %v", err)
		}
		otherCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		otherSpaceRan := false
		if err := store.WithWorkspaceModelWrite(otherCtx, "space-b", func(WorkspaceModelWriter) error {
			otherSpaceRan = true
			return nil
		}); err != nil || !otherSpaceRan {
			t.Fatalf("unrelated workspace should proceed: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	testBudgetBindingPriceRace(t, pool, store, binding, primary, fallback)
}

type pauseBeforeModelCommit struct {
	Store
	ready   chan struct{}
	release chan struct{}
}

func (s pauseBeforeModelCommit) WithWorkspaceModelWrite(
	ctx context.Context, workspaceID string, write func(WorkspaceModelWriter) error,
) error {
	return s.Store.WithWorkspaceModelWrite(ctx, workspaceID, func(w WorkspaceModelWriter) error {
		if err := write(w); err != nil {
			return err
		}
		close(s.ready)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.release:
			return nil
		}
	})
}

func testBudgetBindingPriceRace(t *testing.T, pool *pgxpool.Pool, store *PostgresStore, binding ProjectBinding, primary, fallback string) {
	t.Helper()
	app := "asterflow_price_race_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	cfg := pool.Config()
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	otherPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherPool.Close)

	s := NewService(store, nil)
	for _, bindFirst := range []bool{true, false} {
		name := "price update commits first"
		wantBudget, wantPrice := 0.0, 0.0
		var wantSecond error = ErrInvalidArgument
		if bindFirst {
			name = "budget binding commits first"
			wantBudget, wantPrice = 100, 4
			wantSecond = ErrDeploymentPriceRequired
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			request := BindProjectRequest{WorkspaceID: binding.WorkspaceID, Env: binding.Env, ModelProfileVersionID: binding.ModelProfileVersionID}
			if _, err := s.BindProject(ctx, request); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{primary, fallback} {
				if _, err := s.UpdateDeploymentPricing(ctx, id, UpdateDeploymentPricingRequest{
					WorkspaceID: binding.WorkspaceID, OutputPricePerMillion: 4,
				}); err != nil {
					t.Fatal(err)
				}
			}
			request.MonthlyBudget = 100
			bind := func(s *Service) error {
				_, err := s.BindProject(ctx, request)
				return err
			}
			clearPrice := func(s *Service) error {
				_, err := s.UpdateDeploymentPricing(ctx, fallback, UpdateDeploymentPricingRequest{
					WorkspaceID: binding.WorkspaceID, CachedInputPricePerMillion: 0.25,
				})
				return err
			}

			first, second := bind, clearPrice
			if !bindFirst {
				first, second = clearPrice, bind
			}
			paused := pauseBeforeModelCommit{Store: store, ready: make(chan struct{}), release: make(chan struct{})}
			var unblock sync.Once
			release := func() { unblock.Do(func() { close(paused.release) }) }
			defer release()
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { firstDone <- first(NewService(paused, nil)) }()
			select {
			case <-paused.ready:
			case err := <-firstDone:
				t.Fatalf("first write finished without pausing under its lock: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go func() { secondDone <- second(NewService(NewPostgresStore(otherPool), nil)) }()

			ticker := time.NewTicker(12 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')", app).Scan(&waiting)
				if err != nil {
					t.Fatalf("observe writer waiting for lock: %v", err)
				}
				if waiting {
					break
				}
				select {
				case err := <-secondDone:
					t.Fatalf("second writer did not wait for the workspace lock: %v", err)
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			release()
			select {
			case err := <-firstDone:
				if err != nil {
					t.Fatalf("first write failed: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case err := <-secondDone:
				if !errors.Is(err, wantSecond) {
					t.Fatalf("second write must fail after seeing the first commit: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			got, err := s.GetProjectBinding(ctx, binding.WorkspaceID, binding.Env)
			if err != nil || got.MonthlyBudget != wantBudget {
				t.Fatalf("unexpected final binding: %+v (%v)", got, err)
			}
			deployment, err := store.GetDeployment(ctx, fallback)
			if err != nil || deployment.OutputPricePerMillion != wantPrice {
				t.Fatalf("unexpected final price: %+v (%v)", deployment, err)
			}
		})
	}
}
