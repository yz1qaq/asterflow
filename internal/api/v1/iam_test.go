package v1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
)

// Embedded methods are deliberately unused; this fixture controls only handler dependencies.
type handlerStoreFixture struct {
	iam.Store
	failure       error
	limit, offset int32
}

func (s *handlerStoreFixture) GetUserByEmail(context.Context, string) (*domain.User, error) {
	return nil, s.failure
}
func (s *handlerStoreFixture) CreateUser(context.Context, *domain.User) error {
	return s.failure
}
func (s *handlerStoreFixture) ListUsers(_ context.Context, limit, offset int32) ([]*domain.User, error) {
	s.limit, s.offset = limit, offset
	if s.failure != nil {
		return nil, s.failure
	}
	return []*domain.User{}, nil
}

func serveHandler(method, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	router := gin.New()
	router.Handle(method, "/", handler)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestIAMRejectsInvalidBodiesBeforeBusinessCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &IAMHandler{}
	for name, method := range map[string]gin.HandlerFunc{
		"login":  handler.Login,
		"create": handler.CreateUser,
	} {
		for _, body := range []string{
			"", "{", "[]",
			"{\"unexpected\":true}",
			"{} {}",
			"{} trailing",
			"{\"email\":123}",
			"{\"email\":\"" + strings.Repeat("x", maxJSONBodyBytes) + "\"}",
		} {
			t.Run(name, func(t *testing.T) {
				response := serveHandler(http.MethodPost, "/", body, method)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("invalid body returned %d", response.Code)
				}
			})
		}
	}
}

func TestUserListPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query         string
		status        int
		limit, offset int32
	}{
		{"", 200, 50, 0},
		{"?limit=10&offset=20", 200, 10, 20},
		{"?limit=200&offset=2147483647", 200, 200, 2147483647},
		{"?limit=0", 400, 0, 0},
		{"?limit=201", 400, 0, 0},
		{"?limit=abc", 400, 0, 0},
		{"?offset=-1", 400, 0, 0},
		{"?offset=2147483648", 400, 0, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			store := &handlerStoreFixture{}
			handler := &IAMHandler{service: iam.NewService(store, nil)}
			response := serveHandler(http.MethodGet, "/"+tc.query, "", handler.ListUsers)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d", response.Code, tc.status)
			}
			if tc.status == 200 && (store.limit != tc.limit || store.offset != tc.offset ||
				strings.TrimSpace(response.Body.String()) != "[]") {
				t.Fatalf("pagination or empty array response mismatch: %d/%d", store.limit, store.offset)
			}
		})
	}
}

func TestIAMInternalErrorsDoNotExposeDatabaseDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &handlerStoreFixture{failure: errors.New("private SQL and connection details")}
	handler := &IAMHandler{service: iam.NewService(store, []byte("handler-fixture-key-at-least-32-bytes"))}
	for name, tc := range map[string]struct {
		method string
		body   string
		handle gin.HandlerFunc
	}{
		"login":  {http.MethodPost, "{\"email\":\"user@example.test\",\"password\":\"password123\"}", handler.Login},
		"create": {http.MethodPost, "{\"email\":\"user@example.test\",\"password\":\"password123\",\"name\":\"User\"}", handler.CreateUser},
		"list":   {http.MethodGet, "", handler.ListUsers},
	} {
		t.Run(name, func(t *testing.T) {
			response := serveHandler(tc.method, "/", tc.body, tc.handle)
			if response.Code != 500 || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("internal error response = %d %s", response.Code, response.Body.String())
			}
		})
	}
	store.failure = iam.ErrEmailExists
	response := serveHandler(http.MethodPost, "/",
		"{\"email\":\"user@example.test\",\"password\":\"password123\",\"name\":\"User\"}", handler.CreateUser)
	if response.Code != http.StatusConflict {
		t.Fatalf("email conflict status = %d", response.Code)
	}
}
