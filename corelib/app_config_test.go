package corelib

import (
	"encoding/json"
	"testing"
)

// TestAppConfig_UnmarshalIgnoresUnknownExtraToolKeys verifies that when a
// config JSON contains an "extra_tool_configs" map with OEM-specific keys
// (e.g. "tigerclaw") but the current brand is the default brand, loading the
// config does NOT produce an error. Go's encoding/json silently ignores
// unknown top-level fields and correctly deserialises map entries regardless
// of the current brand.
//
// Validates: Requirements 9.4, 9.2, 9.3
func TestAppConfig_UnmarshalIgnoresUnknownExtraToolKeys(t *testing.T) {
	raw := `{
		"claude": {"current_model": "sonnet"},
		"extra_tool_configs": {
			"tigerclaw": {
				"current_model": "tc-v1",
				"models": [{"name": "tc-v1", "command": "tigerclaw"}]
			},
			"some_future_oem_tool": {
				"current_model": "future-1"
			}
		}
	}`

	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("expected no error unmarshalling config with OEM extra_tool_configs, got: %v", err)
	}

	// The extra_tool_configs map should be populated with the keys from JSON.
	if cfg.ExtraToolConfigs == nil {
		t.Fatal("ExtraToolConfigs should not be nil after unmarshal")
	}
	if _, ok := cfg.ExtraToolConfigs["tigerclaw"]; !ok {
		t.Error("ExtraToolConfigs should contain 'tigerclaw' key")
	}
	if tc := cfg.ExtraToolConfigs["tigerclaw"]; tc.CurrentModel != "tc-v1" {
		t.Errorf("tigerclaw CurrentModel = %q, want %q", tc.CurrentModel, "tc-v1")
	}
	if _, ok := cfg.ExtraToolConfigs["some_future_oem_tool"]; !ok {
		t.Error("ExtraToolConfigs should contain 'some_future_oem_tool' key")
	}

	// Verify that standard fields still deserialise correctly alongside extra configs.
	if cfg.Claude.CurrentModel != "sonnet" {
		t.Errorf("Claude.CurrentModel = %q, want %q", cfg.Claude.CurrentModel, "sonnet")
	}
}

// TestAppConfig_UnmarshalIgnoresCompletelyUnknownTopLevelKeys verifies that
// truly unknown top-level JSON keys (not mapped to any struct field) are
// silently ignored by Go's json.Unmarshal.
//
// Validates: Requirements 9.4
func TestAppConfig_UnmarshalIgnoresCompletelyUnknownTopLevelKeys(t *testing.T) {
	raw := `{
		"claude": {"current_model": "sonnet"},
		"totally_unknown_field": "should be ignored",
		"another_unknown": 42
	}`

	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("expected no error with unknown top-level keys, got: %v", err)
	}

	if cfg.Claude.CurrentModel != "sonnet" {
		t.Errorf("Claude.CurrentModel = %q, want %q", cfg.Claude.CurrentModel, "sonnet")
	}
}
