package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestGetModelRouteDirectAndAlias(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	t.Run("direct", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-chat", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("alias", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4.1", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for alias, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestGetModelRouteNotFound(t *testing.T) {
	h := &Handler{}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models/not-exists", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}
