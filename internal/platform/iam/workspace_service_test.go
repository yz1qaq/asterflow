package iam

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

func TestWorkspaceService(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	service := NewService(store, nil)
	ctx := context.Background()
	alice := &domain.User{ID: uuid.NewString(), Email: "service-alice@example.test"}
	bob := &domain.User{ID: uuid.NewString(), Email: "service-bob@example.test"}
	admin := &domain.User{ID: uuid.NewString(), Email: "service-admin@example.test", Role: GlobalRoleAdmin}
	for _, user := range []*domain.User{alice, bob, admin} {
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	aliceSpace, err := service.CreateWorkspaceForUser(ctx, alice.ID, " Alice Space ", "description", nil)
	if err != nil || aliceSpace.Name != "Alice Space" || aliceSpace.CreatedAt.IsZero() {
		t.Fatalf("create workspace: %v", err)
	}
	bobSpace, err := service.CreateWorkspaceForUser(ctx, bob.ID, "Bob Space", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if role, err := service.WorkspaceRole(ctx, alice.ID, aliceSpace.ID); err != nil || role != WorkspaceRoleOwner {
		t.Fatalf("creator role: %s %v", role, err)
	}
	if role, err := service.WorkspaceRole(ctx, bob.ID, bobSpace.ID); err != nil || role != WorkspaceRoleOwner {
		t.Fatalf("second creator role: %s %v", role, err)
	}
	if _, err := service.WorkspaceRole(ctx, alice.ID, bobSpace.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-space membership: %v", err)
	}
	own, err := service.ListAccessibleWorkspaces(ctx, alice.ID, 0, 0)
	if err != nil || len(own) != 1 || own[0].ID != aliceSpace.ID {
		t.Fatalf("ordinary user scope: %v", err)
	}
	all, err := service.ListAccessibleWorkspaces(ctx, admin.ID, 50, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("admin scope: %v", err)
	}
	if role, err := service.WorkspaceRole(ctx, admin.ID, aliceSpace.ID); err != nil || role != WorkspaceRoleAdmin {
		t.Fatalf("effective admin role: %s %v", role, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workspace_members WHERE user_id=$1", admin.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("global admin access must not insert membership")
	}
	if _, err := pgstore.New(pool).UpsertWorkspaceMember(ctx, pgstore.UpsertWorkspaceMemberParams{WorkspaceID: aliceSpace.ID, UserID: uuid.MustParse(bob.ID), Role: WorkspaceRoleViewer}); err != nil {
		t.Fatal(err)
	}
	if role, err := service.WorkspaceRole(ctx, bob.ID, aliceSpace.ID); err != nil || role != WorkspaceRoleViewer {
		t.Fatalf("viewer role: %s %v", role, err)
	}
	if _, err := service.CreateWorkspaceForUser(ctx, alice.ID, "  ", "", nil); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("blank workspace name: %v", err)
	}
	if _, err := service.CreateWorkspaceForUser(ctx, uuid.NewString(), "Invalid Actor", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing actor: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET status='inactive' WHERE id=$1", alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateWorkspaceForUser(ctx, alice.ID, "Blocked", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("inactive creation: %v", err)
	}
	if _, err := service.ListAccessibleWorkspaces(ctx, alice.ID, 50, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("inactive list: %v", err)
	}
	if _, err := service.WorkspaceRole(ctx, alice.ID, aliceSpace.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("inactive role: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.ListAccessibleWorkspaces(canceled, bob.ID, 50, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("database cause: %v", err)
	}
}

func TestWorkspaceMembershipManagement(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	service := NewService(store, nil)
	ctx := context.Background()
	users := make([]*domain.User, 5)
	for i := range users {
		users[i] = &domain.User{ID: uuid.NewString(), Email: uuid.NewString() + "@example.test"}
		if i == 4 {
			users[i].Role = GlobalRoleAdmin
		}
		if err := store.CreateUser(ctx, users[i]); err != nil {
			t.Fatal(err)
		}
	}
	owner, admin, viewer, target, global := users[0], users[1], users[2], users[3], users[4]
	ws, err := service.CreateWorkspaceForUser(ctx, owner.ID, "Management", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	put := func(actor, user, role string, want error) {
		t.Helper()
		got, err := service.UpsertWorkspaceMember(ctx, actor, ws.ID, user, role)
		if !errors.Is(err, want) {
			t.Fatalf("put role %s: got %v want %v", role, err, want)
		}
		if want == nil && (got.Role != role || got.CreatedAt.IsZero() || got.Email == "") {
			t.Fatal("incomplete member result")
		}
	}
	put(owner.ID, admin.ID, WorkspaceRoleAdmin, nil)
	put(owner.ID, viewer.ID, WorkspaceRoleViewer, nil)
	put(admin.ID, target.ID, WorkspaceRoleEditor, nil)
	put(admin.ID, target.ID, WorkspaceRoleOwner, ErrForbidden)
	put(admin.ID, owner.ID, WorkspaceRoleMember, ErrForbidden)
	put(admin.ID, owner.ID, WorkspaceRoleOwner, ErrForbidden)
	put(viewer.ID, target.ID, WorkspaceRoleMember, ErrForbidden)
	if _, err := service.ListWorkspaceMembers(ctx, viewer.ID, ws.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if members, err := service.ListWorkspaceMembers(ctx, admin.ID, ws.ID); err != nil || len(members) != 4 {
		t.Fatalf("list: %v", err)
	}
	put(owner.ID, target.ID, "invalid-role", ErrInvalidWorkspaceRole)
	put(owner.ID, "invalid-id", WorkspaceRoleMember, ErrInvalidUserID)
	put(owner.ID, uuid.NewString(), WorkspaceRoleMember, ErrUserNotFound)
	put(global.ID, owner.ID, WorkspaceRoleMember, ErrLastOwner)
	put(global.ID, target.ID, WorkspaceRoleOwner, nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{owner.ID, target.ID} {
		go func(id string) {
			<-start
			_, err := service.UpsertWorkspaceMember(ctx, global.ID, ws.ID, id, WorkspaceRoleMember)
			results <- err
		}(id)
	}
	close(start)
	successes, blocked := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrLastOwner):
			blocked++
		default:
			t.Fatalf("concurrent demotion: %v", err)
		}
	}
	if successes != 1 || blocked != 1 {
		t.Fatalf("successes=%d blocked=%d", successes, blocked)
	}
	var owners int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND role='owner'", ws.ID).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners=%d: %v", owners, err)
	}
	missing := &domain.WorkspaceMember{WorkspaceID: uuid.NewString(), UserID: owner.ID, Role: WorkspaceRoleMember}
	if err := store.UpsertWorkspaceMemberGuarded(ctx, global.ID, missing); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("missing space: %v", err)
	}
}

func TestWorkspaceMemberDeletion(t *testing.T) {
	pool := newIAMTestPool(t)
	store := NewPostgresStore(pool)
	service := NewService(store, nil)
	ctx := context.Background()
	createUser := func(role string) *domain.User {
		t.Helper()
		u := &domain.User{ID: uuid.NewString(), Email: uuid.NewString() + "@example.test", Role: role}
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	owner := createUser(GlobalRoleMember)
	admin := createUser(GlobalRoleMember)
	viewer := createUser(GlobalRoleMember)
	target := createUser(GlobalRoleMember)
	global := createUser(GlobalRoleAdmin)
	ws, err := service.CreateWorkspaceForUser(ctx, owner.ID, "Deletion", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	put := func(user *domain.User, role string) {
		t.Helper()
		if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, ws.ID, user.ID, role); err != nil {
			t.Fatal(err)
		}
	}
	put(admin, WorkspaceRoleAdmin)
	put(viewer, WorkspaceRoleViewer)
	put(target, WorkspaceRoleMember)
	check := func(actor, user string, want error) {
		t.Helper()
		if err := service.DeleteWorkspaceMember(ctx, actor, ws.ID, user); !errors.Is(err, want) {
			t.Fatalf("delete got %v want %v", err, want)
		}
	}
	check(viewer.ID, target.ID, ErrForbidden)
	check(admin.ID, owner.ID, ErrForbidden)
	check(global.ID, owner.ID, ErrLastOwner)
	check(admin.ID, "invalid", ErrInvalidUserID)
	check(admin.ID, target.ID, nil)
	check(admin.ID, target.ID, ErrWorkspaceMemberNotFound)
	if _, err := store.GetUserByID(ctx, target.ID); err != nil {
		t.Fatal("member deletion must retain user")
	}
	for _, mixed := range []bool{false, true} {
		ws2, err := service.CreateWorkspaceForUser(ctx, owner.ID, "Concurrent", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, ws2.ID, target.ID, WorkspaceRoleOwner); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() { <-start; results <- service.DeleteWorkspaceMember(ctx, global.ID, ws2.ID, owner.ID) }()
		go func() {
			<-start
			if mixed {
				_, err := service.UpsertWorkspaceMember(ctx, global.ID, ws2.ID, target.ID, WorkspaceRoleMember)
				results <- err
			} else {
				results <- service.DeleteWorkspaceMember(ctx, global.ID, ws2.ID, target.ID)
			}
		}()
		close(start)
		success, blocked := 0, 0
		for range 2 {
			err := <-results
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrLastOwner):
				blocked++
			default:
				t.Fatal(err)
			}
		}
		if success != 1 || blocked != 1 {
			t.Fatalf("mixed=%v success=%d blocked=%d", mixed, success, blocked)
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND role='owner'", ws2.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("owners=%d error=%v", count, err)
		}
	}
}

func TestMembershipRechecksActorAfterWorkspaceLock(t *testing.T) {
	for _, scenario := range []struct{ name, actorRole, operation string }{
		{"owner cannot promote self after demotion", WorkspaceRoleOwner, "self"},
		{"admin cannot edit after demotion", WorkspaceRoleAdmin, "edit"},
		{"removed owner cannot delete another member", WorkspaceRoleOwner, "delete"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pool := newIAMTestPool(t)
			store := NewPostgresStore(pool)
			service := NewService(store, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			users := make([]*domain.User, 3)
			for i := range users {
				users[i] = &domain.User{ID: uuid.NewString(), Email: uuid.NewString() + "@example.test"}
				if err := store.CreateUser(ctx, users[i]); err != nil {
					t.Fatal(err)
				}
			}
			owner, actor, target := users[0], users[1], users[2]
			ws, err := service.CreateWorkspaceForUser(ctx, owner.ID, "Revocation", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range []struct{ id, role string }{{actor.ID, scenario.actorRole}, {target.ID, WorkspaceRoleMember}} {
				if _, err := service.UpsertWorkspaceMember(ctx, owner.ID, ws.ID, item.id, item.role); err != nil {
					t.Fatal(err)
				}
			}
			actorConfig := pool.Config()
			applicationName := "asterflow_wait_" + uuid.NewString()
			actorConfig.ConnConfig.RuntimeParams["application_name"] = applicationName
			actorPool, err := pgxpool.NewWithConfig(ctx, actorConfig)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(actorPool.Close)
			actorService := NewService(NewPostgresStore(actorPool), nil)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			q := store.q.WithTx(tx)
			if _, err := q.LockWorkspace(ctx, ws.ID); err != nil {
				t.Fatal(err)
			}
			if scenario.operation == "delete" {
				if _, err := q.DeleteWorkspaceMember(ctx, pgstore.DeleteWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: uuid.MustParse(actor.ID)}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := q.UpsertWorkspaceMember(ctx, pgstore.UpsertWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: uuid.MustParse(actor.ID), Role: WorkspaceRoleViewer}); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan error, 1)
			go func() {
				if scenario.operation == "delete" {
					done <- actorService.DeleteWorkspaceMember(ctx, actor.ID, ws.ID, target.ID)
					return
				}
				targetID, role := target.ID, WorkspaceRoleEditor
				if scenario.operation == "self" {
					targetID, role = actor.ID, WorkspaceRoleOwner
				}
				_, err := actorService.UpsertWorkspaceMember(ctx, actor.ID, ws.ID, targetID, role)
				done <- err
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			waiting := false
			for !waiting {
				select {
				case err := <-done:
					t.Fatalf("request finished before acquiring workspace lock: %v", err)
				case <-ctx.Done():
					t.Fatal("request never reached workspace lock")
				case <-ticker.C:
					if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')", applicationName).Scan(&waiting); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("revoked actor must be rejected after lock, got %v", err)
				}
			case <-ctx.Done():
				t.Fatal("request did not finish after lock release")
			}
			member, err := store.GetWorkspaceMember(ctx, ws.ID, target.ID)
			if err != nil || member.Role != WorkspaceRoleMember {
				t.Fatal("revoked actor changed target membership")
			}
			if scenario.operation != "delete" {
				member, err := store.GetWorkspaceMember(ctx, ws.ID, actor.ID)
				if err != nil || member.Role != WorkspaceRoleViewer {
					t.Fatal("revoked actor restored its own privileges")
				}
			}
		})
	}
}
