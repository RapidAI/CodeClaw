package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeSettingsPath returns ~/.claude/settings.json
func ClaudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// internalOnlyFields are fields that cc-switch filters out before writing
// to Claude Code's settings.json — they are not recognized by Claude Code
// and can cause unexpected behavior.
var internalOnlyFields = []string{
	"api_format", "apiFormat",
	"openrouter_compat_mode", "openrouterCompatMode",
}

// WriteClaudeSettings writes ~/.claude/settings.json with the provider's
// env configuration. This is what cc-switch does instead of relying solely
// on process environment variables.
//
// The settings.json approach is more stable because:
// 1. Claude Code reads it on startup and on internal subprocess restarts
// 2. Environment variables can be lost when Claude Code spawns child processes
// 3. It persists across terminal sessions
func WriteClaudeSettings(apiKey, baseURL, modelID string) error {
	if apiKey == "" {
		return nil // builtin provider, skip
	}

	settingsPath := ClaudeSettingsPath()

	// Read existing settings to preserve user's manual config (MCP, permissions, etc.)
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Build env map
	env, _ := existing["env"].(map[string]interface{})
	if env == nil {
		env = make(map[string]interface{})
	}

	env["ANTHROPIC_AUTH_TOKEN"] = apiKey
	if baseURL != "" {
		env["ANTHROPIC_BASE_URL"] = baseURL
	}
	if modelID != "" {
		env["ANTHROPIC_MODEL"] = modelID
		// cc-switch normalizes these: set all model slots to the same value
		// for third-party API compatibility
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelID
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelID
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelID
		// Clean up deprecated field
		delete(env, "ANTHROPIC_SMALL_FAST_MODEL")
	}

	existing["env"] = env

	// Remove internal-only fields that Claude Code doesn't recognize
	for _, field := range internalOnlyFields {
		delete(existing, field)
	}

	return AtomicWriteJSON(settingsPath, existing)
}

// ReadClaudeSettings reads the current ~/.claude/settings.json for backfill.
func ReadClaudeSettings() (map[string]interface{}, error) {
	data, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read claude settings: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse claude settings: %w", err)
	}
	return result, nil
}
