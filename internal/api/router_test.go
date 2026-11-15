package api

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRouter(t *testing.T) {
	handler := NewRouter()
	if handler == nil {
		t.Fatal("NewRouter returned nil")
	}
	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"health", http.MethodGet, "/healthz", http.StatusOK},
		{"wrong_method", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{"unknown", http.MethodGet, "/missing", http.StatusNotFound},
		{"not_a_prefix", http.MethodGet, "/healthz/extra", http.StatusNotFound},
		{"trailing_slash", http.MethodGet, "/healthz/", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("want status %d, got %d", tc.wantStatus, recorder.Code)
			}
			if tc.wantStatus == http.StatusOK {
				if got := recorder.Body.String(); got != "ok" {
					t.Fatalf("want body ok, got %q", got)
				}
				media, _, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
				if err != nil || media != "text/plain" {
					t.Fatalf("want text/plain, got %q, %v",
						recorder.Header().Get("Content-Type"), err)
				}
			}
		})
	}
}
