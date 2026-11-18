package modelconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var _ Store = (*PostgresStore)(nil)

func TestProviderAccountService(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	cipher, err := NewCipher([]byte("test-only-key"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, cipher)
	req := CreateProviderAccountRequest{WorkspaceID: " space-a ", Name: " primary ", Kind: "openai-compatible", BaseURL: " https://example.invalid/v1 ", APIKey: " test-only-api-key ", CreatedBy: "actor-a"}
	account, err := service.CreateProviderAccount(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if account.Name != "primary" || account.WorkspaceID != "space-a" || account.BaseURL != "https://example.invalid/v1" || !account.HasAPIKey || account.Status != "active" || account.CreatedAt.IsZero() {
		t.Fatal("public account fields incorrect")
	}
	record, err := store.GetProviderAccount(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(record.APIKeyCiphertext, []byte(req.APIKey)) {
		t.Fatal("plaintext stored")
	}
	plain, err := cipher.Decrypt(record.APIKeyCiphertext)
	if err != nil || plain != req.APIKey {
		t.Fatal("opaque key changed")
	}
	raw, err := json.Marshal(account)
	if err != nil || strings.Contains(string(raw), `"api_key"`) || strings.Contains(string(raw), req.APIKey) || strings.Contains(string(raw), "ciphertext") {
		t.Fatal("credential leaked into public response")
	}
	if _, err := service.CreateProviderAccount(ctx, req); !errors.Is(err, ErrProviderAccountNameExists) {
		t.Fatal("duplicate cause lost")
	}
	list, err := service.ListProviderAccounts(ctx, " space-a ")
	if err != nil || len(list) != 1 || list[0].ID != account.ID {
		t.Fatal("account list mismatch")
	}
	list, err = service.ListProviderAccounts(ctx, "space-b")
	if err != nil || list == nil || len(list) != 0 {
		t.Fatal("workspace list isolation mismatch")
	}
	for _, bad := range []string{"", "   ", "ftp://example.invalid", "/v1", "https://user:secret@example.invalid/v1", "https://example.invalid/v1#fragment"} {
		invalid := req
		invalid.Name = uuid.NewString()
		invalid.BaseURL = bad
		if _, err := service.CreateProviderAccount(ctx, invalid); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal("invalid endpoint accepted")
		}
	}
	for _, field := range []string{"workspace", "name", "kind", "key"} {
		invalid := req
		switch field {
		case "workspace":
			invalid.WorkspaceID = " "
		case "name":
			invalid.Name = " "
		case "kind":
			invalid.Kind = " "
		case "key":
			invalid.APIKey = " "
		}
		if _, err := service.CreateProviderAccount(ctx, invalid); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal("blank required field accepted")
		}
	}
	if _, err := service.ListProviderAccounts(ctx, " "); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("blank list workspace accepted")
	}
	if err := service.RotateProviderAPIKey(ctx, "space-b", account.ID, "replacement"); !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatal("cross-workspace rotation accepted")
	}
	unchanged, err := store.GetProviderAccount(ctx, account.ID)
	if err != nil || !bytes.Equal(unchanged.APIKeyCiphertext, record.APIKeyCiphertext) {
		t.Fatal("failed rotation changed credentials")
	}
	if err := service.RotateProviderAPIKey(ctx, "space-a", account.ID, "replacement"); err != nil {
		t.Fatal(err)
	}
	rotated, err := store.GetProviderAccount(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err = cipher.Decrypt(rotated.APIKeyCiphertext)
	if err != nil || plain != "replacement" {
		t.Fatal("new key not persisted")
	}
	if err := service.RotateProviderAPIKey(ctx, "space-a", uuid.NewString(), "new"); !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatal("missing account cause lost")
	}
	if err := service.RotateProviderAPIKey(ctx, "space-a", "invalid", "new"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("invalid ID accepted")
	}
	if err := service.RotateProviderAPIKey(ctx, "space-a", account.ID, " "); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("blank key accepted")
	}
	var bound CreateProviderAccountRequest
	if err := json.Unmarshal([]byte(`{"workspace_id":"injected","created_by":"injected","name":"visible"}`), &bound); err != nil {
		t.Fatal(err)
	}
	if bound.WorkspaceID != "" || bound.CreatedBy != "" || bound.Name != "visible" {
		t.Fatal("untrusted JSON sets server-owned identity fields")
	}
}

func TestDeploymentService(t *testing.T) {
	pool := newModelConfigTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES ('space-a','A'),('space-b','B')"); err != nil {
		t.Fatal(err)
	}
	st := NewPostgresStore(pool)
	cipher, err := NewCipher([]byte("test-only-master"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st, cipher)
	a, err := s.CreateProviderAccount(ctx, CreateProviderAccountRequest{WorkspaceID: "space-a", Name: "main", Kind: "openai-compatible", BaseURL: "https://example.invalid/v1", APIKey: "test-only-key"})
	if err != nil {
		t.Fatal(err)
	}
	req := CreateDeploymentRequest{WorkspaceID: " space-a ", ProviderAccountID: a.ID, Name: " main ", ModelName: " example-model ", Region: " test ", MaxRetries: -1, InputPricePerMillion: 1.25, OutputPricePerMillion: 2.5, CachedInputPricePerMillion: 0.1, CreatedBy: "actor"}
	d, err := s.CreateDeployment(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "main" || d.ModelName != "example-model" || d.Region != "test" || d.TimeoutMS != 120000 || d.MaxRetries != 0 || d.CreatedAt.IsZero() {
		t.Fatal("normalization/defaults/database timestamp incorrect")
	}
	list, err := s.ListDeployments(ctx, "space-a")
	if err != nil || len(list) != 1 || list[0].ID != d.ID {
		t.Fatal("list mismatch")
	}
	list, err = s.ListDeployments(ctx, "space-b")
	if err != nil || list == nil || len(list) != 0 {
		t.Fatal("workspace isolation mismatch")
	}
	if _, err := s.CreateDeployment(ctx, req); !errors.Is(err, ErrDeploymentNameExists) {
		t.Fatal("duplicate name error lost")
	}
	cross := req
	cross.WorkspaceID = "space-b"
	if _, err := s.CreateDeployment(ctx, cross); !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatal("cross workspace account accepted")
	}
	update := UpdateDeploymentPricingRequest{WorkspaceID: "space-a", InputPricePerMillion: 3.5, OutputPricePerMillion: 5, CachedInputPricePerMillion: 0.25}
	updated, err := s.UpdateDeploymentPricing(ctx, d.ID, update)
	if err != nil || updated.InputPricePerMillion != 3.5 || updated.OutputPricePerMillion != 5 || updated.CachedInputPricePerMillion != 0.25 {
		t.Fatal("pricing update mismatch")
	}
	persisted, err := st.GetDeployment(ctx, d.ID)
	if err != nil || !reflect.DeepEqual(updated, persisted) {
		t.Fatal("update result must reflect database timestamps")
	}
	crossUpdate := update
	crossUpdate.WorkspaceID = "space-b"
	if _, err := s.UpdateDeploymentPricing(ctx, d.ID, crossUpdate); !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatal("cross workspace update accepted")
	}
	if _, err := s.UpdateDeploymentPricing(ctx, uuid.NewString(), update); !errors.Is(err, ErrDeploymentNotFound) {
		t.Fatal("missing deployment error lost")
	}
	if _, err := s.UpdateDeploymentPricing(ctx, "invalid", update); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("invalid update ID accepted")
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		invalid := req
		invalid.InputPricePerMillion = bad
		if _, err := s.CreateDeployment(ctx, invalid); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal("invalid create price accepted")
		}
		invalidUpdate := update
		invalidUpdate.OutputPricePerMillion = bad
		if _, err := s.UpdateDeploymentPricing(ctx, d.ID, invalidUpdate); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal("invalid update price accepted")
		}
	}
	for _, field := range []string{"workspace", "name", "model", "account", "timeout", "retry"} {
		invalid := req
		switch field {
		case "workspace":
			invalid.WorkspaceID = " "
		case "name":
			invalid.Name = " "
		case "model":
			invalid.ModelName = " "
		case "account":
			invalid.ProviderAccountID = "invalid"
		case "timeout":
			invalid.TimeoutMS = math.MaxInt32 + 1
		case "retry":
			invalid.MaxRetries = math.MaxInt32 + 1
		}
		if _, err := s.CreateDeployment(ctx, invalid); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal("invalid deployment input accepted")
		}
	}
	if _, err := s.ListDeployments(ctx, " "); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("blank workspace accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ListDeployments(canceled, "space-a"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation cause lost")
	}
}

func TestProfileCreateService(t *testing.T) {
	ctx := context.Background()
	pool := newModelConfigTestPool(t)
	m, e := os.ReadFile("../../../migrations/000004_model_profiles.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(m)); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES('space-a','A'),('space-b','B')"); e != nil {
		t.Fatal(e)
	}
	store := NewPostgresStore(pool)
	service := NewService(store, nil)
	makeDeployment := func(space, name string) *ModelDeployment {
		a := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: space, Name: name, Kind: "example", BaseURL: "https://example.invalid", Status: "active"}}
		if e := store.CreateProviderAccount(ctx, a); e != nil {
			t.Fatal(e)
		}
		d := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: space, ProviderAccountID: a.ID, Name: name, ModelName: "example", TimeoutMS: 120000, Status: "active"}
		if e := store.CreateDeployment(ctx, d); e != nil {
			t.Fatal(e)
		}
		return d
	}
	primary := makeDeployment("space-a", "primary")
	fallback := makeDeployment("space-a", "fallback")
	other := makeDeployment("space-b", "other")
	req := CreateProfileRequest{WorkspaceID: " space-a ", Name: " support ", Description: " description ", PrimaryDeploymentID: " " + strings.ReplaceAll(primary.ID, "-", "") + " ", FallbackDeploymentIDs: []string{" " + strings.ReplaceAll(fallback.ID, "-", "") + " ", fallback.ID}, CreatedBy: "actor-a"}
	before := append([]string(nil), req.FallbackDeploymentIDs...)
	p, v, e := service.CreateProfile(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	if p.Name != "support" || p.WorkspaceID != "space-a" || p.Description != "description" || p.CreatedBy != "actor-a" || p.CreatedAt.IsZero() || v.ProfileID != p.ID || v.Version != 1 || v.PrimaryDeploymentID != primary.ID || v.ClassificationMax != "internal" || v.CreatedBy != "actor-a" || v.CreatedAt.IsZero() {
		t.Fatal("persisted fields mismatch")
	}
	if !reflect.DeepEqual(v.FallbackDeploymentIDs, []string{fallback.ID, fallback.ID}) || !reflect.DeepEqual(req.FallbackDeploymentIDs, before) {
		t.Fatal("fallback normalization mutated caller or removed order/duplicates")
	}
	persisted, e := store.GetProfileVersion(ctx, v.ID)
	if e != nil || !reflect.DeepEqual(persisted, v) {
		t.Fatal("version differs from database")
	}
	if _, _, e = service.CreateProfile(ctx, req); !errors.Is(e, ErrProfileNameExists) {
		t.Fatalf("duplicate cause: %v", e)
	}
	blankFallback := req
	blankFallback.Name = "empty-fallbacks"
	blankFallback.FallbackDeploymentIDs = nil
	_, empty, e := service.CreateProfile(ctx, blankFallback)
	if e != nil || empty.FallbackDeploymentIDs == nil || len(empty.FallbackDeploymentIDs) != 0 {
		t.Fatal("empty fallbacks not []")
	}
	custom := req
	custom.Name = "classified"
	custom.ClassificationMax = " secret "
	_, classified, e := service.CreateProfile(ctx, custom)
	if e != nil || classified.ClassificationMax != "secret" {
		t.Fatal("classification not trimmed")
	}
	count := func() int {
		t.Helper()
		ps, e := store.ListProfiles(ctx, "space-a")
		if e != nil {
			t.Fatal(e)
		}
		return len(ps)
	}
	start := count()
	cases := []struct {
		name string
		edit func(*CreateProfileRequest)
		want error
	}{
		{"workspace", func(r *CreateProfileRequest) { r.WorkspaceID = " " }, ErrInvalidArgument},
		{"name", func(r *CreateProfileRequest) { r.Name = " " }, ErrInvalidArgument},
		{"primary-empty", func(r *CreateProfileRequest) { r.PrimaryDeploymentID = "" }, ErrInvalidArgument},
		{"primary-invalid", func(r *CreateProfileRequest) { r.PrimaryDeploymentID = "invalid" }, ErrInvalidArgument},
		{"fallback-empty", func(r *CreateProfileRequest) { r.FallbackDeploymentIDs = []string{""} }, ErrInvalidArgument},
		{"fallback-invalid", func(r *CreateProfileRequest) { r.FallbackDeploymentIDs = []string{"invalid"} }, ErrInvalidArgument},
		{"primary-missing", func(r *CreateProfileRequest) { r.PrimaryDeploymentID = uuid.NewString() }, ErrDeploymentNotFound},
		{"fallback-missing", func(r *CreateProfileRequest) { r.FallbackDeploymentIDs = []string{uuid.NewString()} }, ErrDeploymentNotFound},
		{"primary-cross-space", func(r *CreateProfileRequest) { r.PrimaryDeploymentID = other.ID }, ErrDeploymentNotFound},
		{"fallback-cross-space", func(r *CreateProfileRequest) { r.FallbackDeploymentIDs = []string{other.ID} }, ErrDeploymentNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := req
			bad.Name = tc.name
			tc.edit(&bad)
			p, v, e := service.CreateProfile(ctx, bad)
			if !errors.Is(e, tc.want) || p != nil || v != nil {
				t.Fatalf("got p=%v v=%v err=%v", p, v, e)
			}
			if count() != start {
				t.Fatal("rejected request persisted a profile")
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, e = service.CreateProfile(canceled, req); !errors.Is(e, context.Canceled) {
		t.Fatalf("lost cancellation: %v", e)
	}
}

func TestProfileVersionService(t *testing.T) {
	ctx := context.Background()
	pool := newModelConfigTestPool(t)
	migration, e := os.ReadFile("../../../migrations/000004_model_profiles.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO workspaces(id,name) VALUES('space-a','A'),('space-b','B')"); e != nil {
		t.Fatal(e)
	}
	st := NewPostgresStore(pool)
	s := NewService(st, nil)
	deployment := func(space string) *ModelDeployment {
		a := &providerAccountRecord{ProviderAccount: ProviderAccount{ID: uuid.NewString(), WorkspaceID: space, Name: "main", Kind: "example", BaseURL: "https://example.invalid", Status: "active"}}
		if e := st.CreateProviderAccount(ctx, a); e != nil {
			t.Fatal(e)
		}
		d := &ModelDeployment{ID: uuid.NewString(), WorkspaceID: space, ProviderAccountID: a.ID, Name: "main", ModelName: "example", TimeoutMS: 120000, Status: "active"}
		if e := st.CreateDeployment(ctx, d); e != nil {
			t.Fatal(e)
		}
		return d
	}
	d := deployment("space-a")
	other := deployment("space-b")
	p, v1, e := s.CreateProfile(ctx, CreateProfileRequest{WorkspaceID: "space-a", Name: "support", PrimaryDeploymentID: d.ID})
	if e != nil {
		t.Fatal(e)
	}
	p2, _, e := s.CreateProfile(ctx, CreateProfileRequest{WorkspaceID: "space-b", Name: "support", PrimaryDeploymentID: other.ID})
	if e != nil {
		t.Fatal(e)
	}
	list, e := s.ListProfiles(ctx, " space-a ")
	if e != nil || len(list) != 1 || !reflect.DeepEqual(list[0], p) {
		t.Fatalf("list: %v", e)
	}
	empty, e := s.ListProfiles(ctx, "empty-space")
	if e != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list: %v", e)
	}
	if _, e = s.ListProfiles(ctx, " "); !errors.Is(e, ErrInvalidArgument) {
		t.Fatalf("empty workspace: %v", e)
	}
	got, e := s.GetProfile(ctx, " space-a ", " "+strings.ReplaceAll(p.ID, "-", "")+" ")
	if e != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("get normalized: %v", e)
	}
	for _, id := range []string{p2.ID, uuid.NewString()} {
		if _, e = s.GetProfile(ctx, "space-a", id); !errors.Is(e, ErrProfileNotFound) {
			t.Fatalf("foreign/missing get: %v", e)
		}
		if _, e = s.ListProfileVersions(ctx, "space-a", id); !errors.Is(e, ErrProfileNotFound) {
			t.Fatalf("foreign/missing list: %v", e)
		}
	}
	if _, e = s.GetProfile(ctx, "space-a", "invalid"); !errors.Is(e, ErrInvalidArgument) {
		t.Fatalf("bad profile id: %v", e)
	}
	if _, e = s.GetProfile(ctx, "", p.ID); !errors.Is(e, ErrInvalidArgument) {
		t.Fatalf("blank workspace: %v", e)
	}
	req := CreateProfileVersionRequest{WorkspaceID: " space-a ", PrimaryDeploymentID: d.ID, FallbackDeploymentIDs: []string{" " + strings.ReplaceAll(d.ID, "-", "") + " "}, ClassificationMax: " confidential ", CreatedBy: "actor-a"}
	before := append([]string(nil), req.FallbackDeploymentIDs...)
	v2, e := s.CreateProfileVersion(ctx, " "+p.ID+" ", req)
	if e != nil || v2.Version != 2 || v2.ProfileID != p.ID || v2.ClassificationMax != "confidential" || v2.CreatedBy != "actor-a" || v2.CreatedAt.IsZero() {
		t.Fatalf("append: %v", e)
	}
	if !reflect.DeepEqual(req.FallbackDeploymentIDs, before) || !reflect.DeepEqual(v2.FallbackDeploymentIDs, []string{d.ID}) {
		t.Fatal("fallback normalization mismatch")
	}
	req.ClassificationMax = " "
	req.FallbackDeploymentIDs = nil
	v3, e := s.CreateProfileVersion(ctx, p.ID, req)
	if e != nil || v3.Version != 3 || v3.ClassificationMax != "internal" || v3.FallbackDeploymentIDs == nil {
		t.Fatalf("defaults: %v", e)
	}
	versions, e := s.ListProfileVersions(ctx, "space-a", p.ID)
	if e != nil || len(versions) != 3 || !reflect.DeepEqual(versions[0], v1) {
		t.Fatalf("list or v1 mutation: %v", e)
	}
	cases := []struct {
		name, id string
		req      CreateProfileVersionRequest
		want     error
	}{
		{"foreign-parent", p2.ID, req, ErrProfileNotFound},
		{"missing-parent", uuid.NewString(), req, ErrProfileNotFound},
		{"invalid-parent", "invalid", req, ErrInvalidArgument},
	}
	for _, tc := range cases {
		if _, e = s.CreateProfileVersion(ctx, tc.id, tc.req); !errors.Is(e, tc.want) {
			t.Fatalf("%s: %v", tc.name, e)
		}
	}
	for _, primary := range []string{other.ID, uuid.NewString(), "invalid", ""} {
		bad := req
		bad.PrimaryDeploymentID = primary
		want := ErrDeploymentNotFound
		if primary == "" || primary == "invalid" {
			want = ErrInvalidArgument
		}
		if _, e = s.CreateProfileVersion(ctx, p.ID, bad); !errors.Is(e, want) {
			t.Fatalf("bad primary: %v", e)
		}
	}
	bad := req
	bad.FallbackDeploymentIDs = []string{other.ID}
	if _, e = s.CreateProfileVersion(ctx, p.ID, bad); !errors.Is(e, ErrDeploymentNotFound) {
		t.Fatalf("foreign fallback: %v", e)
	}
	versions, e = s.ListProfileVersions(ctx, "space-a", p.ID)
	if e != nil || len(versions) != 3 {
		t.Fatal("rejected append persisted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.CreateProfileVersion(canceled, p.ID, req); !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled: %v", e)
	}
}
