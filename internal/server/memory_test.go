package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemoryAdminRequiresTokenEvenWhenCodeWritesDisabled(t *testing.T) {
	for _, tc := range []struct {
		configured, provided string
		status               int
	}{{"", "", 401}, {"secret", "", 401}, {"secret", "wrong", 401}, {"secret", "secret", 204}} {
		s := &Server{adminToken: tc.configured, write: false}
		handler := s.requireMemoryAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		r := httptest.NewRequest("GET", "/api/v1/memories", nil)
		if tc.provided != "" {
			r.Header.Set("Authorization", "Bearer "+tc.provided)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("token configuration %q: got %d want %d", tc.configured, w.Code, tc.status)
		}
	}
}
