package config

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// ─── GetModelConfig edge cases ───────────────────────────────────────

func TestGetModelConfigDeepSeekChat(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-chat")
	if !ok {
		t.Fatal("expected ok for deepseek-chat")
	}
	if thinking || search {
		t.Fatalf("expected no thinking/search for deepseek-chat, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekReasoner(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-reasoner")
	if !ok {
		t.Fatal("expected ok for deepseek-reasoner")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekChatSearch(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-chat-search")
	if !ok {
		t.Fatal("expected ok for deepseek-chat-search")
	}
	if thinking || !search {
		t.Fatalf("expected thinking=false search=true, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekReasonerSearch(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-reasoner-search")
	if !ok {
		t.Fatal("expected ok for deepseek-reasoner-search")
	}
	if !thinking || !search {
		t.Fatalf("expected both true, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigCaseInsensitive(t *testing.T) {
	thinking, search, ok := GetModelConfig("DeepSeek-Chat")
	if !ok {
		t.Fatal("expected ok for case-insensitive deepseek-chat")
	}
	if thinking || search {
		t.Fatalf("expected no thinking/search for case-insensitive deepseek-chat")
	}
}

func TestGetModelConfigUnknownModel(t *testing.T) {
	_, _, ok := GetModelConfig("gpt-4")
	if ok {
		t.Fatal("expected not ok for unknown model")
	}
}

func TestGetModelConfigEmpty(t *testing.T) {
	_, _, ok := GetModelConfig("")
	if ok {
		t.Fatal("expected not ok for empty model")
	}
}

// ─── lower function ──────────────────────────────────────────────────

func TestLowerFunction(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "hello"},
		{"ALLCAPS", "allcaps"},
		{"already-lower", "already-lower"},
		{"Mixed-CASE-123", "mixed-case-123"},
		{"", ""},
	}
	for _, tc := range tests {
		got := lower(tc.input)
		if got != tc.expected {
			t.Errorf("lower(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ─── Config.MarshalJSON / UnmarshalJSON roundtrip ────────────────────

func TestConfigJSONRoundtrip(t *testing.T) {
	cfg := Config{
		Keys:     []string{"key1", "key2"},
		Accounts: []Account{{Email: "user@example.com", Password: "pass", Token: "tok"}},
		ClaudeMapping: map[string]string{
			"fast": "deepseek-chat",
			"slow": "deepseek-reasoner",
		},
		VercelSyncHash: "hash123",
		VercelSyncTime: 1234567890,
		AdditionalFields: map[string]any{
			"custom_field": "custom_value",
		},
	}

	data, err := cfg.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded.Keys) != 2 || decoded.Keys[0] != "key1" {
		t.Fatalf("unexpected keys: %#v", decoded.Keys)
	}
	if len(decoded.Accounts) != 1 || decoded.Accounts[0].Email != "user@example.com" {
		t.Fatalf("unexpected accounts: %#v", decoded.Accounts)
	}
	if decoded.ClaudeMapping["fast"] != "deepseek-chat" {
		t.Fatalf("unexpected claude mapping: %#v", decoded.ClaudeMapping)
	}
	if decoded.VercelSyncHash != "hash123" {
		t.Fatalf("unexpected vercel sync hash: %q", decoded.VercelSyncHash)
	}
	if decoded.AdditionalFields["custom_field"] != "custom_value" {
		t.Fatalf("unexpected additional fields: %#v", decoded.AdditionalFields)
	}
}

func TestConfigUnmarshalJSONPreservesUnknownFields(t *testing.T) {
	raw := `{"keys":["k1"],"accounts":[],"my_custom_field":"hello","number_field":42}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if cfg.AdditionalFields["my_custom_field"] != "hello" {
		t.Fatalf("expected custom field preserved, got %#v", cfg.AdditionalFields)
	}
	// number_field should also be preserved
	if cfg.AdditionalFields["number_field"] != float64(42) {
		t.Fatalf("expected number field preserved, got %#v", cfg.AdditionalFields["number_field"])
	}
}

// ─── Config.Clone ────────────────────────────────────────────────────

func TestConfigCloneIsDeepCopy(t *testing.T) {
	cfg := Config{
		Keys:     []string{"key1"},
		Accounts: []Account{{Email: "user@test.com", Token: "token"}},
		ClaudeMapping: map[string]string{
			"fast": "deepseek-chat",
		},
		AdditionalFields: map[string]any{"custom": "value"},
	}

	cloned := cfg.Clone()

	// Modify original
	cfg.Keys[0] = "modified"
	cfg.Accounts[0].Email = "modified@test.com"
	cfg.ClaudeMapping["fast"] = "modified-model"

	// Cloned should not be affected
	if cloned.Keys[0] != "key1" {
		t.Fatalf("clone keys was affected by original change: %#v", cloned.Keys)
	}
	if cloned.Accounts[0].Email != "user@test.com" {
		t.Fatalf("clone accounts was affected: %#v", cloned.Accounts)
	}
	if cloned.ClaudeMapping["fast"] != "deepseek-chat" {
		t.Fatalf("clone claude mapping was affected: %#v", cloned.ClaudeMapping)
	}
}

func TestConfigCloneNilMaps(t *testing.T) {
	cfg := Config{
		Keys:     []string{"k"},
		Accounts: nil,
	}
	cloned := cfg.Clone()
	if len(cloned.Keys) != 1 {
		t.Fatalf("unexpected keys length: %d", len(cloned.Keys))
	}
	if cloned.Accounts != nil {
		t.Fatalf("expected nil accounts in clone, got %#v", cloned.Accounts)
	}
}

// ─── Account.Identifier edge cases ───────────────────────────────────

func TestAccountIdentifierPreferenceMobileOverToken(t *testing.T) {
	acc := Account{Mobile: "13800138000", Token: "tok"}
	if acc.Identifier() != "13800138000" {
		t.Fatalf("expected mobile identifier, got %q", acc.Identifier())
	}
}

func TestAccountIdentifierPreferenceEmailOverMobile(t *testing.T) {
	acc := Account{Email: "user@test.com", Mobile: "13800138000"}
	if acc.Identifier() != "user@test.com" {
		t.Fatalf("expected email identifier, got %q", acc.Identifier())
	}
}

func TestAccountIdentifierEmptyAccount(t *testing.T) {
	acc := Account{}
	if acc.Identifier() != "" {
		t.Fatalf("expected empty identifier for empty account, got %q", acc.Identifier())
	}
}

// ─── normalizeConfigInput ────────────────────────────────────────────

func TestNormalizeConfigInputStripsQuotes(t *testing.T) {
	got := normalizeConfigInput(`"base64:abc"`)
	if strings.HasPrefix(got, `"`) || strings.HasSuffix(got, `"`) {
		t.Fatalf("expected quotes stripped, got %q", got)
	}
}

func TestNormalizeConfigInputStripsSingleQuotes(t *testing.T) {
	got := normalizeConfigInput("'some-value'")
	if strings.HasPrefix(got, "'") || strings.HasSuffix(got, "'") {
		t.Fatalf("expected single quotes stripped, got %q", got)
	}
}

func TestNormalizeConfigInputTrimsWhitespace(t *testing.T) {
	got := normalizeConfigInput("  hello  ")
	if got != "hello" {
		t.Fatalf("expected trimmed, got %q", got)
	}
}

// ─── parseConfigString edge cases ────────────────────────────────────

func TestParseConfigStringPlainJSON(t *testing.T) {
	cfg, err := parseConfigString(`{"keys":["k1"],"accounts":[]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0] != "k1" {
		t.Fatalf("unexpected keys: %#v", cfg.Keys)
	}
}

func TestParseConfigStringBase64Prefix(t *testing.T) {
	rawJSON := `{"keys":["base64-key"],"accounts":[]}`
	b64 := base64.StdEncoding.EncodeToString([]byte(rawJSON))
	cfg, err := parseConfigString("base64:" + b64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0] != "base64-key" {
		t.Fatalf("unexpected keys: %#v", cfg.Keys)
	}
}

func TestParseConfigStringInvalidBase64(t *testing.T) {
	_, err := parseConfigString("base64:!!!invalid!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestParseConfigStringEmptyString(t *testing.T) {
	_, err := parseConfigString("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

// ─── Store methods ───────────────────────────────────────────────────

func TestStoreSnapshotReturnsClone(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"u@test.com","token":"t1"}]}`)
	store := LoadStore()
	snap := store.Snapshot()
	snap.Keys[0] = "modified"
	if store.Keys()[0] != "k1" {
		t.Fatal("snapshot modification should not affect store")
	}
}

func TestStoreHasAPIKeyMultipleKeys(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["key1","key2","key3"],"accounts":[]}`)
	store := LoadStore()
	if !store.HasAPIKey("key1") {
		t.Fatal("expected key1 found")
	}
	if !store.HasAPIKey("key2") {
		t.Fatal("expected key2 found")
	}
	if !store.HasAPIKey("key3") {
		t.Fatal("expected key3 found")
	}
	if store.HasAPIKey("nonexistent") {
		t.Fatal("expected nonexistent key not found")
	}
}

func TestStoreFindAccountNotFound(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"u@test.com"}]}`)
	store := LoadStore()
	_, ok := store.FindAccount("nonexistent@test.com")
	if ok {
		t.Fatal("expected account not found")
	}
}

func TestStoreCompatWideInputStrictOutputDefaultTrue(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	if !store.CompatWideInputStrictOutput() {
		t.Fatal("expected default wide_input_strict_output=true when unset")
	}
}

func TestStoreCompatWideInputStrictOutputCanDisable(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[],"compat":{"wide_input_strict_output":false}}`)
	store := LoadStore()
	if store.CompatWideInputStrictOutput() {
		t.Fatal("expected wide_input_strict_output=false when explicitly configured")
	}

	snap := store.Snapshot()
	data, err := snap.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	rawCompat, ok := out["compat"].(map[string]any)
	if !ok {
		t.Fatalf("expected compat in marshaled output, got %#v", out)
	}
	if rawCompat["wide_input_strict_output"] != false {
		t.Fatalf("expected explicit false in compat, got %#v", rawCompat)
	}
}

func TestStoreIsEnvBacked(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	if !store.IsEnvBacked() {
		t.Fatal("expected env-backed store")
	}
}

func TestStoreReplace(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	newCfg := Config{
		Keys:     []string{"new-key"},
		Accounts: []Account{{Email: "new@test.com"}},
	}
	if err := store.Replace(newCfg); err != nil {
		t.Fatalf("replace error: %v", err)
	}
	if !store.HasAPIKey("new-key") {
		t.Fatal("expected new key after replace")
	}
	if store.HasAPIKey("k1") {
		t.Fatal("expected old key removed after replace")
	}
}

func TestStoreUpdate(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	store := LoadStore()
	err := store.Update(func(cfg *Config) error {
		cfg.Keys = append(cfg.Keys, "k2")
		return nil
	})
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if !store.HasAPIKey("k2") {
		t.Fatal("expected k2 after update")
	}
}

func TestStoreClaudeMapping(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":[],"accounts":[],"claude_mapping":{"fast":"deepseek-chat","slow":"deepseek-reasoner"}}`)
	store := LoadStore()
	mapping := store.ClaudeMapping()
	if mapping["fast"] != "deepseek-chat" {
		t.Fatalf("unexpected fast mapping: %q", mapping["fast"])
	}
	if mapping["slow"] != "deepseek-reasoner" {
		t.Fatalf("unexpected slow mapping: %q", mapping["slow"])
	}
}

func TestStoreClaudeMappingEmpty(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":[],"accounts":[]}`)
	store := LoadStore()
	mapping := store.ClaudeMapping()
	// Even without config mapping, there are defaults
	if mapping == nil {
		t.Fatal("expected non-nil mapping (may contain defaults)")
	}
}

func TestStoreSetVercelSync(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":[],"accounts":[]}`)
	store := LoadStore()
	if err := store.SetVercelSync("hash123", 1234567890); err != nil {
		t.Fatalf("setVercelSync error: %v", err)
	}
	snap := store.Snapshot()
	if snap.VercelSyncHash != "hash123" || snap.VercelSyncTime != 1234567890 {
		t.Fatalf("unexpected vercel sync: hash=%q time=%d", snap.VercelSyncHash, snap.VercelSyncTime)
	}
}

func TestStoreExportJSONAndBase64(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["export-key"],"accounts":[]}`)
	store := LoadStore()
	jsonStr, b64Str, err := store.ExportJSONAndBase64()
	if err != nil {
		t.Fatalf("export error: %v", err)
	}
	if !strings.Contains(jsonStr, "export-key") {
		t.Fatalf("expected JSON to contain key: %q", jsonStr)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64Str)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	if !strings.Contains(string(decoded), "export-key") {
		t.Fatalf("expected base64-decoded to contain key: %q", string(decoded))
	}
}

// ─── OpenAIModelsResponse / ClaudeModelsResponse ─────────────────────

func TestOpenAIModelsResponse(t *testing.T) {
	resp := OpenAIModelsResponse()
	if resp["object"] != "list" {
		t.Fatalf("unexpected object: %v", resp["object"])
	}
	data, ok := resp["data"].([]ModelInfo)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp["data"])
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty models list")
	}
}

func TestClaudeModelsResponse(t *testing.T) {
	resp := ClaudeModelsResponse()
	if resp["object"] != "list" {
		t.Fatalf("unexpected object: %v", resp["object"])
	}
	data, ok := resp["data"].([]ModelInfo)
	if !ok {
		t.Fatalf("unexpected data type: %T", resp["data"])
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty models list")
	}
}
