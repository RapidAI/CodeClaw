package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
)

func newResolverWithConfigJSON(t *testing.T, cfgJSON string) (*config.Store, *auth.Resolver) {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", cfgJSON)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := auth.NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "unused", nil
	})
	return store, resolver
}

func TestEmbeddingsRouteContract(t *testing.T) {
	store, resolver := newResolverWithConfigJSON(t, `{"embeddings":{"provider":"deterministic"}}`)
	h := &Handler{Store: store, Auth: resolver}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	t.Run("unauthorized", func(t *testing.T) {
		body := bytes.NewBufferString(`{"model":"gpt-4o","input":"hello"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("ok", func(t *testing.T) {
		body := bytes.NewBufferString(`{"model":"gpt-4o","input":["a","b"]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", body)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response failed: %v", err)
		}
		if out["object"] != "list" {
			t.Fatalf("unexpected object: %#v", out["object"])
		}
		data, _ := out["data"].([]any)
		if len(data) != 2 {
			t.Fatalf("expected 2 embeddings, got %d", len(data))
		}
	})
}

func TestEmbeddingsRouteProviderMissing(t *testing.T) {
	store, resolver := newResolverWithConfigJSON(t, `{}`)
	h := &Handler{Store: store, Auth: resolver}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	body := bytes.NewBufferString(`{"model":"gpt-4o","input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	errObj, _ := out["error"].(map[string]any)
	if _, ok := errObj["code"]; !ok {
		t.Fatalf("expected error.code in response: %#v", out)
	}
	if _, ok := errObj["param"]; !ok {
		t.Fatalf("expected error.param in response: %#v", out)
	}
}
