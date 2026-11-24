package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
)

// lookupFixture supplies database outcomes; JWT verification uses the real IAM service.
type lookupFixture struct {
	iam.Store
	role, status string
	err          error
	calls        int
}

func (s *lookupFixture) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	s.calls++
	if ctx.Value("fixture") != "forwarded" {
		return nil, errors.New("request context lost")
	}
	if s.err != nil {
		return nil, s.err
	}
	return &domain.User{ID: id, Role: s.role, Status: s.status}, nil
}
func TestAuthAndGlobalAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := []byte("middleware-fixture-key-32-bytes!!")
	claims := &iam.Claims{UserID: "user-1", RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "asterflow", Subject: "user-1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, header, role, status string
		err                        error
		want, calls                int
	}{
		{"missing", "", "admin", "active", nil, 401, 0},
		{"malformed", "Basic abc", "admin", "active", nil, 401, 0},
		{"invalid", "Bearer invalid", "admin", "active", nil, 401, 0},
		{"valid admin", "Bearer " + signed, "admin", "active", nil, 200, 1},
		{"valid lower scheme", "bearer " + signed, "admin", "active", nil, 200, 1},
		{"member", "Bearer " + signed, "member", "active", nil, 403, 1},
		{"inactive", "Bearer " + signed, "admin", "inactive", nil, 401, 1},
		{"missing user", "Bearer " + signed, "admin", "active", iam.ErrUserNotFound, 401, 1},
		{"database failure", "Bearer " + signed, "admin", "active", errors.New("offline"), 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &lookupFixture{role: tc.role, status: tc.status, err: tc.err}
			service := iam.NewService(store, key)
			router := gin.New()
			hit := false
			router.GET("/", Auth(service), RequireGlobalAdmin(), func(c *gin.Context) {
				hit = true
				if GetUserIDFromContext(c.Request.Context()) != "user-1" || GetGlobalRoleFromContext(c.Request.Context()) != "admin" {
					t.Error("trusted identity missing")
				}
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(context.WithValue(request.Context(), "fixture", "forwarded"))
			if tc.header != "" {
				request.Header.Set("Authorization", tc.header)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want || hit != (tc.want == 200) || store.calls != tc.calls {
				t.Fatalf("status=%d hit=%t calls=%d", response.Code, hit, store.calls)
			}
		})
	}
}

func TestWorkspaceMethodPolicy(t *testing.T) {
	for _, tc := range []struct {
		role, method, path string
		allowed            bool
	}{
		{iam.WorkspaceRoleViewer, "GET", "/api/v1/models", true},
		{iam.WorkspaceRoleViewer, "POST", "/api/v1/models", false},
		{iam.WorkspaceRoleViewer, "DELETE", "/api/v1/conversations/:conversation_id", false},
		{iam.WorkspaceRoleMember, "DELETE", "/api/v1/conversations/:conversation_id", true},
		{iam.WorkspaceRoleMember, "DELETE", "/api/v1/conversations/:conversation_id/other", false},
		{iam.WorkspaceRoleMember, "DELETE", "/api/v1/model-accounts/:account_id", false},
		{iam.WorkspaceRoleMember, "POST", "/api/v1/models", false},
		{iam.WorkspaceRoleMember, "POST", "/api/v1/agents/a/chat", true},
		{iam.WorkspaceRoleMember, "POST", "/api/v1/kbs/k/search", true},
		{iam.WorkspaceRoleEditor, "POST", "/api/v1/models", true},
		{iam.WorkspaceRoleEditor, "POST", "/api/v1/approvals/a/approve", false},
		{iam.WorkspaceRoleAdmin, "POST", "/api/v1/approvals/a/approve", true},
		{iam.WorkspaceRoleOwner, "POST", "/api/v1/conversations/c/a2ui/actions", true},
		{"unknown", "GET", "/api/v1/models", false},
	} {
		if got := workspaceMethodAllowed(tc.role, tc.method, tc.path); got != tc.allowed {
			t.Errorf("%s %s %s: got=%v", tc.role, tc.method, tc.path, got)
		}
	}
}

type workspaceLookupFixture struct {
	iam.Store
	workspaceErr, memberErr error
}

func (s *workspaceLookupFixture) GetUserByID(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "user-1", Role: iam.GlobalRoleMember, Status: "active"}, nil
}
func (s *workspaceLookupFixture) GetWorkspaceByID(context.Context, string) (*domain.Workspace, error) {
	if s.workspaceErr != nil {
		return nil, s.workspaceErr
	}
	return &domain.Workspace{ID: "space-1"}, nil
}
func (s *workspaceLookupFixture) GetWorkspaceMember(context.Context, string, string) (*domain.WorkspaceMember, error) {
	if s.memberErr != nil {
		return nil, s.memberErr
	}
	return &domain.WorkspaceMember{WorkspaceID: "space-1", UserID: "user-1", Role: iam.WorkspaceRoleViewer}, nil
}
func TestWorkspaceMiddlewareErrorsAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name                         string
		authenticated, withWorkspace bool
		workspaceErr, memberErr      error
		want                         int
	}{
		{"missing identity", false, true, nil, nil, 401},
		{"missing workspace", true, false, nil, nil, 400},
		{"missing space", true, true, iam.ErrWorkspaceNotFound, nil, 404},
		{"not member", true, true, nil, iam.ErrWorkspaceMemberNotFound, 403},
		{"workspace database failure", true, true, errors.New("private SQL details"), nil, 500},
		{"membership database failure", true, true, nil, errors.New("private SQL details"), 500},
		{"authorized context", true, true, nil, nil, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := iam.NewService(&workspaceLookupFixture{workspaceErr: tc.workspaceErr, memberErr: tc.memberErr}, nil)
			router := gin.New()
			hit := false
			router.GET("/", func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), "fixture", "forwarded")
				if tc.authenticated {
					ctx = context.WithValue(ctx, contextKeyUserID, "user-1")
				}
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			}, Workspace(service), func(c *gin.Context) {
				hit = true
				ctx := c.Request.Context()
				if GetUserIDFromContext(ctx) != "user-1" || GetWorkspaceIDFromContext(ctx) != "space-1" || GetWorkspaceRoleFromContext(ctx) != iam.WorkspaceRoleViewer || ctx.Value("fixture") != "forwarded" {
					t.Error("trusted context missing")
				}
				c.Status(200)
			})
			req := httptest.NewRequest("GET", "/", nil)
			if tc.withWorkspace {
				req.Header.Set("X-Workspace-ID", "space-1")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want || hit != (tc.want == 200) {
				t.Fatalf("status=%d want=%d hit=%v", rec.Code, tc.want, hit)
			}
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatal("internal error exposed")
			}
		})
	}
}
