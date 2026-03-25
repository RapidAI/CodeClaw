package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAccountIdentifierFallsBackToTokenHash(t *testing.T) {
	acc := Account{Token: "example-token-value"}
	id := acc.Identifier()
	if !strings.HasPrefix(id, "token:") {
		t.Fatalf("expected token-prefixed identifier, got %q", id)
	}
	if len(id) != len("token:")+16 {
		t.Fatalf("unexpected identifier length: %d (%q)", len(id), id)
	}
}

func TestStoreFindAccountWithTokenOnlyIdentifier(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["k1"],
		"accounts":[{"token":"token-only-account"}]
	}`)

	store := LoadStore()
	accounts := store.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	id := accounts[0].Identifier()
	if id == "" {
		t.Fatalf("expected synthetic identifier for token-only account")
	}
	found, ok := store.FindAccount(id)
	if !ok {
		t.Fatalf("expected FindAccount to locate token-only account by synthetic id")
	}
	if found.Token != "token-only-account" {
		t.Fatalf("unexpected token value: %q", found.Token)
	}
}

func TestStoreUpdateAccountTokenKeepsOldAndNewIdentifierResolvable(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"accounts":[{"token":"old-token"}]
	}`)

	store := LoadStore()
	before := store.Accounts()
	if len(before) != 1 {
		t.Fatalf("expected 1 account, got %d", len(before))
	}
	oldID := before[0].Identifier()
	if oldID == "" {
		t.Fatal("expected old identifier")
	}
	if err := store.UpdateAccountToken(oldID, "new-token"); err != nil {
		t.Fatalf("update token failed: %v", err)
	}

	after := store.Accounts()
	newID := after[0].Identifier()
	if newID == "" || newID == oldID {
		t.Fatalf("expected changed identifier, old=%q new=%q", oldID, newID)
	}
	if got, ok := store.FindAccount(newID); !ok || got.Token != "new-token" {
		t.Fatalf("expected find by new identifier")
	}
	if got, ok := store.FindAccount(oldID); !ok || got.Token != "new-token" {
		t.Fatalf("expected find by old identifier alias")
	}
}

func TestLoadStoreRejectsInvalidFieldType(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":"not-array","accounts":[]}`)
	store := LoadStore()
	if len(store.Keys()) != 0 || len(store.Accounts()) != 0 {
		t.Fatalf("expected empty store when config type is invalid")
	}
}

func TestParseConfigStringSupportsQuotedBase64Prefix(t *testing.T) {
	rawJSON := `{"keys":["k1"],"accounts":[{"email":"u@example.com","password":"p"}]}`
	b64 := base64.StdEncoding.EncodeToString([]byte(rawJSON))
	cfg, err := parseConfigString(`"base64:` + b64 + `"`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0] != "k1" {
		t.Fatalf("unexpected keys: %#v", cfg.Keys)
	}
}

func TestParseConfigStringSupportsRawURLBase64(t *testing.T) {
	rawJSON := `{"keys":["k-url"],"accounts":[]}`
	b64 := base64.RawURLEncoding.EncodeToString([]byte(rawJSON))
	cfg, err := parseConfigString(b64)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0] != "k-url" {
		t.Fatalf("unexpected keys: %#v", cfg.Keys)
	}
}

func TestLoadConfigOnVercelWithoutConfigFileFallsBackToMemory(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_CONFIG_JSON", "")
	t.Setenv("CONFIG_JSON", "")
	t.Setenv("DS2API_CONFIG_PATH", "testdata/does-not-exist.json")

	cfg, fromEnv, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fromEnv {
		t.Fatalf("expected fromEnv=true for vercel fallback")
	}
	if len(cfg.Keys) != 0 || len(cfg.Accounts) != 0 {
		t.Fatalf("expected empty bootstrap config, got keys=%d accounts=%d", len(cfg.Keys), len(cfg.Accounts))
	}
}
