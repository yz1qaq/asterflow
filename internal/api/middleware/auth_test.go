package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
