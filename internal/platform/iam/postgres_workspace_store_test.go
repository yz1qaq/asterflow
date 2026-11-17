package iam

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

func TestWorkspaceReadStore(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	queries := pgstore.New(pool)
	ctx := context.Background()
	alice := &domain.User{ID: uuid.NewString(), Email: "alice-workspace@example.test"}
	bob := &domain.User{ID: uuid.NewString(), Email: "bob-workspace@example.test"}
	for _, user := range []*domain.User{alice, bob} {
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("empty lists encode as arrays", func(t *testing.T) {
		all, err := store.ListWorkspaces(ctx, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		own, err := store.ListUserWorkspaces(ctx, alice.ID, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, list := range [][]*domain.Workspace{all, own} {
			data, err := json.Marshal(list)
			if err != nil || string(data) != "[]" {
				t.Fatal("empty workspace list must encode as []")
			}
		}
	})

	for _, id := range []string{"parent", "space-a", "space-b", "space-z"} {
		parent := pgtype.Text{}
		if id == "space-z" {
			parent = pgtype.Text{String: "parent", Valid: true}
		}
		if _, err := queries.CreateWorkspace(ctx, pgstore.CreateWorkspaceParams{
			ID: id, Name: id, Description: "read fixture", ParentID: parent,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE workspaces SET created_at='2026-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO workspace_members(workspace_id,user_id,role) VALUES('space-a',$1,'owner'),('space-z',$1,'viewer'),('space-b',$2,'owner')",
		alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}

	t.Run("nullable parent and returned object are independent", func(t *testing.T) {
		child, err := store.GetWorkspaceByID(ctx, "space-z")
		if err != nil || child.ParentID == nil || *child.ParentID != "parent" || child.Name != "space-z" || child.CreatedAt.IsZero() {
			t.Fatalf("workspace mapping failed: %v", err)
		}
		*child.ParentID = "caller-change"
		child.Name = "caller-change"
		again, err := store.GetWorkspaceByID(ctx, "space-z")
		if err != nil || again.Name != "space-z" || again.ParentID == nil || *again.ParentID != "parent" {
			t.Fatalf("caller mutation affected stored data: %v", err)
		}
		root, err := store.GetWorkspaceByID(ctx, "space-a")
		if err != nil || root.ParentID != nil {
			t.Fatalf("NULL parent mapping: %v", err)
		}
	})

	t.Run("stable paging when creation times match", func(t *testing.T) {
		var ids []string
		for _, offset := range []int32{0, 2} {
			rows, err := store.ListWorkspaces(ctx, 2, offset)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
		}
		if !reflect.DeepEqual(ids, []string{"space-z", "space-b", "space-a", "parent"}) {
			t.Fatalf("paging order = %v", ids)
		}
	})

	t.Run("user lists follow memberships without parent inheritance", func(t *testing.T) {
		for _, tc := range []struct {
			userID string
			want   []string
		}{
			{alice.ID, []string{"space-z", "space-a"}},
			{bob.ID, []string{"space-b"}},
			{uuid.NewString(), []string{}},
		} {
			rows, err := store.ListUserWorkspaces(ctx, tc.userID, 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("membership result = %v, want %v", ids, tc.want)
			}
		}
	})

	t.Run("missing ID invalid UUID and cancellation remain distinguishable", func(t *testing.T) {
		if _, err := store.GetWorkspaceByID(ctx, "missing"); !errors.Is(err, ErrWorkspaceNotFound) {
			t.Fatalf("missing workspace: %v", err)
		}
		if _, err := store.ListUserWorkspaces(ctx, "invalid", 10, 0); err == nil {
			t.Fatal("invalid user UUID accepted")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := store.GetWorkspaceByID(canceled, "space-a"); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	})
}

func TestCreateWorkspaceWithOwner(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	owner := &domain.User{ID: uuid.NewString(), Email: "owner-create@example.test"}
	if err := store.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}

	t.Run("workspace and owner commit together", func(t *testing.T) {
		workspace := &domain.Workspace{ID: "new-space", Name: "New Space"}
		if err := store.CreateWorkspaceWithOwner(ctx, workspace, owner.ID); err != nil {
			t.Fatal(err)
		}
		if workspace.CreatedAt.IsZero() || workspace.UpdatedAt.IsZero() {
			t.Fatal("database timestamps not returned after commit")
		}
		stored, err := store.GetWorkspaceByID(ctx, workspace.ID)
		if err != nil || stored.Name != workspace.Name {
			t.Fatalf("committed workspace missing: %v", err)
		}
		var role string
		if err := pool.QueryRow(ctx, "SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2", workspace.ID, owner.ID).Scan(&role); err != nil || role != WorkspaceRoleOwner {
			t.Fatalf("owner not committed: %v", err)
		}
	})

	t.Run("owner failure rolls back the workspace without modifying input", func(t *testing.T) {
		workspace := &domain.Workspace{ID: "orphan-attempt", Name: "Must Roll Back"}
		before := *workspace
		if err := store.CreateWorkspaceWithOwner(ctx, workspace, uuid.NewString()); err == nil {
			t.Fatal("nonexistent owner accepted")
		}
		if !reflect.DeepEqual(*workspace, before) {
			t.Fatal("failed transaction modified caller object")
		}
		if _, err := store.GetWorkspaceByID(ctx, workspace.ID); !errors.Is(err, ErrWorkspaceNotFound) {
			t.Fatalf("failed creation left an orphan workspace: %v", err)
		}
	})

	t.Run("invalid input or duplicate ID cannot replace the existing owner", func(t *testing.T) {
		if err := store.CreateWorkspaceWithOwner(ctx, nil, owner.ID); err == nil {
			t.Fatal("nil workspace accepted")
		}
		if err := store.CreateWorkspaceWithOwner(ctx, &domain.Workspace{ID: "bad-user"}, "invalid"); err == nil {
			t.Fatal("invalid UUID accepted")
		}
		duplicate := &domain.Workspace{ID: "new-space", Name: "Replacement"}
		if err := store.CreateWorkspaceWithOwner(ctx, duplicate, owner.ID); err == nil {
			t.Fatal("duplicate workspace accepted")
		}
		stored, err := store.GetWorkspaceByID(ctx, "new-space")
		if err != nil || stored.Name != "New Space" {
			t.Fatalf("failed duplicate modified workspace: %v", err)
		}
	})
}

func TestWorkspaceMemberReadStore(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	owner := &domain.User{ID: uuid.NewString(), Email: "member-owner@example.test", Name: "Owner"}
	editor := &domain.User{ID: uuid.NewString(), Email: "member-editor@example.test", Name: "Editor"}
	for _, user := range []*domain.User{owner, editor} {
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	workspace := &domain.Workspace{ID: "member-space", Name: "Members"}
	if err := store.CreateWorkspaceWithOwner(ctx, workspace, owner.ID); err != nil {
		t.Fatal(err)
	}
	editorID := uuid.MustParse(editor.ID)
	if _, err := pgstore.New(pool).UpsertWorkspaceMember(ctx, pgstore.UpsertWorkspaceMemberParams{
		WorkspaceID: workspace.ID, UserID: editorID, Role: WorkspaceRoleEditor,
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("joined user details and workspace role", func(t *testing.T) {
		member, err := store.GetWorkspaceMember(ctx, workspace.ID, owner.ID)
		if err != nil || member.WorkspaceID != workspace.ID || member.UserID != owner.ID ||
			member.Email != owner.Email || member.Name != owner.Name || member.Role != WorkspaceRoleOwner || member.CreatedAt.IsZero() {
			t.Fatalf("member mapping failed: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE users SET name='Renamed Owner' WHERE id=$1", owner.ID); err != nil {
			t.Fatal(err)
		}
		again, err := store.GetWorkspaceMember(ctx, workspace.ID, owner.ID)
		if err != nil || again.Name != "Renamed Owner" {
			t.Fatalf("joined identity did not refresh: %v", err)
		}
	})

	t.Run("list contains member identities and empty list stays an array", func(t *testing.T) {
		members, err := store.ListWorkspaceMembers(ctx, workspace.ID)
		if err != nil || len(members) != 2 {
			t.Fatalf("member list count: %v", err)
		}
		found := map[string]string{}
		for _, member := range members {
			if member.Email == "" || member.Name == "" {
				t.Fatal("joined display fields missing")
			}
			found[member.UserID] = member.Role
		}
		if found[owner.ID] != WorkspaceRoleOwner || found[editor.ID] != WorkspaceRoleEditor {
			t.Fatal("workspace roles were confused with global roles")
		}
		empty, err := store.ListWorkspaceMembers(ctx, "empty-space")
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(empty)
		if err != nil || string(data) != "[]" {
			t.Fatal("empty member list must encode as []")
		}
	})

	t.Run("missing relationship invalid user and cancellation", func(t *testing.T) {
		for _, pair := range [][2]string{{"another-space", owner.ID}, {workspace.ID, uuid.NewString()}} {
			if _, err := store.GetWorkspaceMember(ctx, pair[0], pair[1]); !errors.Is(err, ErrWorkspaceMemberNotFound) {
				t.Fatalf("missing member relation: %v", err)
			}
		}
		if _, err := store.GetWorkspaceMember(ctx, workspace.ID, "invalid"); err == nil {
			t.Fatal("invalid UUID accepted")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := store.ListWorkspaceMembers(canceled, workspace.ID); !errors.Is(err, context.Canceled) {
			t.Fatalf("list cancellation cause lost: %v", err)
		}
	})
}

func TestWorkspaceMembershipCancellationAndRollback(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	service := NewService(store, nil)
	ctx := context.Background()
	owner := &domain.User{ID: uuid.NewString(), Email: "lock-owner@example.test"}
	target := &domain.User{ID: uuid.NewString(), Email: "lock-target@example.test"}
	for _, user := range []*domain.User{owner, target} {
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := service.CreateWorkspaceForUser(ctx, owner.ID, "Locked", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, ws.ID, target.ID, WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := store.q.WithTx(tx).LockWorkspace(ctx, ws.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"upsert", "delete"} {
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		if name == "upsert" {
			change := &domain.WorkspaceMember{WorkspaceID: ws.ID, UserID: target.ID, Role: WorkspaceRoleViewer}
			before := *change
			err = store.UpsertWorkspaceMemberGuarded(waitCtx, owner.ID, change)
			if !reflect.DeepEqual(*change, before) {
				t.Fatal("canceled upsert changed input")
			}
		} else {
			_, err = store.DeleteWorkspaceMemberGuarded(waitCtx, owner.ID, ws.ID, target.ID)
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s must wait for workspace lock until canceled: %v", name, err)
		}
		member, readErr := store.GetWorkspaceMember(ctx, ws.ID, target.ID)
		if readErr != nil || member.Role != WorkspaceRoleMember {
			t.Fatalf("%s canceled mutation changed membership", name)
		}
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	invalid := &domain.WorkspaceMember{WorkspaceID: ws.ID, UserID: target.ID, Role: "invalid-role"}
	if err := store.UpsertWorkspaceMemberGuarded(ctx, owner.ID, invalid); err == nil {
		t.Fatal("invalid database role accepted")
	}
	member, err := store.GetWorkspaceMember(ctx, ws.ID, target.ID)
	if err != nil || member.Role != WorkspaceRoleMember {
		t.Fatal("failed role write was not rolled back")
	}
	if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, ws.ID, target.ID, WorkspaceRoleEditor); err != nil {
		t.Fatalf("connection unusable after rollback: %v", err)
	}
}
