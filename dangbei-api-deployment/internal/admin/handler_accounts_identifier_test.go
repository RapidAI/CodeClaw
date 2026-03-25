package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

func newAdminTestHandler(t *testing.T, raw string) *Handler {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", raw)
	t.Setenv("CONFIG_JSON", "")
	store := config.LoadStore()
	return &Handler{
		Store: store,
		Pool:  account.NewPool(store),
	}
}

func TestListAccountsIncludesTokenOnlyIdentifier(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"token":"token-only-account"}]
	}`)

	req := httptest.NewRequest(http.MethodGet, "/admin/accounts?page=1&page_size=10", nil)
	rec := httptest.NewRecorder()
	h.listAccounts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	items, _ := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	first, _ := items[0].(map[string]any)
	identifier, _ := first["identifier"].(string)
	if identifier == "" {
		t.Fatalf("expected non-empty identifier: %#v", first)
	}
	if !strings.HasPrefix(identifier, "token:") {
		t.Fatalf("expected token synthetic identifier, got %q", identifier)
	}
}

func TestDeleteAccountSupportsTokenOnlyIdentifier(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"token":"token-only-account"}]
	}`)
	accounts := h.Store.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	id := accounts[0].Identifier()
	if id == "" {
		t.Fatal("expected token-only synthetic identifier")
	}

	r := chi.NewRouter()
	r.Delete("/admin/accounts/{identifier}", h.deleteAccount)
	req := httptest.NewRequest(http.MethodDelete, "/admin/accounts/"+url.PathEscape(id), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(h.Store.Accounts()); got != 0 {
		t.Fatalf("expected account removed, remaining=%d", got)
	}
}

func TestDeleteAccountSupportsMobileAlias(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"email":"u@example.com","mobile":"13800138000","password":"pwd"}]
	}`)

	r := chi.NewRouter()
	r.Delete("/admin/accounts/{identifier}", h.deleteAccount)
	req := httptest.NewRequest(http.MethodDelete, "/admin/accounts/13800138000", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(h.Store.Accounts()); got != 0 {
		t.Fatalf("expected account removed, remaining=%d", got)
	}
}

func TestFindAccountByIdentifierSupportsMobileAndTokenOnly(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[
			{"email":"u@example.com","mobile":"13800138000","password":"pwd"},
			{"token":"token-only-account"}
		]
	}`)

	accByMobile, ok := findAccountByIdentifier(h.Store, "13800138000")
	if !ok {
		t.Fatal("expected find by mobile")
	}
	if accByMobile.Email != "u@example.com" {
		t.Fatalf("unexpected account by mobile: %#v", accByMobile)
	}

	tokenOnlyID := ""
	for _, acc := range h.Store.Accounts() {
		if strings.TrimSpace(acc.Email) == "" && strings.TrimSpace(acc.Mobile) == "" {
			tokenOnlyID = acc.Identifier()
			break
		}
	}
	if tokenOnlyID == "" {
		t.Fatal("expected token-only account identifier")
	}
	accByTokenOnly, ok := findAccountByIdentifier(h.Store, tokenOnlyID)
	if !ok {
		t.Fatalf("expected find by token-only id=%q", tokenOnlyID)
	}
	if accByTokenOnly.Token != "token-only-account" {
		t.Fatalf("unexpected token-only account: %#v", accByTokenOnly)
	}
}
