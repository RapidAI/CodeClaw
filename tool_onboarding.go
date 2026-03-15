package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureToolOnboardingComplete runs pre-launch onboarding checks for the
// given tool so that first-run interactive prompts (theme selection, trust
// dialogs, safety acknowledgments, etc.) don't block the user every time
// the tool is launched from the app.
//
// Each tool has its own config file and onboarding flags.  This function
// dispatches to the appropriate handler based on the tool name.
//
// The function is idempotent — it only adds missing keys and never
// removes existing user preferences.
func ensureToolOnboardingComplete(app *App, toolName string, projectPath string) {
	var err error
	switch toolName {
	case "claude":
		err = ensureClaudeOnboardingComplete(app, projectPath)
	case "gemini":
		err = ensureGeminiOnboardingComplete(app)
	case "kode":
		err = ensureKodeOnboardingComplete(app, projectPath)
	default:
		// Other tools (codex, iflow, kilo, opencode, cursor) don't have
		// known first-run wizards that need pre-configuration.
		return
	}
	if err != nil && app != nil {
		app.log(fmt.Sprintf("[tool-onboarding] %s pre-check warning: %v", toolName, err))
	}
}

// ensureGeminiOnboardingComplete ensures that Gemini CLI's user-level
// settings file (~/.gemini/settings.json) contains a theme setting so
// the first-run theme selection prompt is skipped.
//
// Gemini CLI shows an interactive theme picker on first launch if no
// theme is configured.  Pre-setting a theme avoids this.
func ensureGeminiOnboardingComplete(app *App) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".gemini")
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			backupPath := configPath + ".bak"
			_ = os.Rename(configPath, backupPath)
			if app != nil {
				app.log(fmt.Sprintf("[gemini-onboarding] backed up corrupt %s to %s", configPath, backupPath))
			}
			existing = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	changed := false

	// Ensure ui.theme is set to skip the theme selection prompt.
	ui, _ := existing["ui"].(map[string]any)
	if ui == nil {
		ui = map[string]any{}
		existing["ui"] = ui
	}
	if ui["theme"] == nil || strings.TrimSpace(fmt.Sprint(ui["theme"])) == "" {
		ui["theme"] = "Default Dark"
		changed = true
	}

	if !changed {
		if app != nil {
			app.log("[gemini-onboarding] settings already complete, no changes needed")
		}
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if app != nil {
		app.log(fmt.Sprintf("[gemini-onboarding] updated %s with theme setting", configPath))
	}
	return nil
}

// ensureKodeOnboardingComplete ensures that Kode CLI's user-level config
// file (~/.kode.json) has onboarding marked as complete so the first-run
// wizard is skipped.  Kode is a fork of Claude Code and has a similar
// onboarding flow.
func ensureKodeOnboardingComplete(app *App, projectPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	configPath := filepath.Join(home, ".kode.json")

	existing := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			backupPath := configPath + ".bak"
			_ = os.Rename(configPath, backupPath)
			if app != nil {
				app.log(fmt.Sprintf("[kode-onboarding] backed up corrupt %s to %s", configPath, backupPath))
			}
			existing = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	changed := false

	if !isTruthy(existing["hasCompletedOnboarding"]) {
		existing["hasCompletedOnboarding"] = true
		changed = true
	}

	if existing["theme"] == nil || strings.TrimSpace(fmt.Sprint(existing["theme"])) == "" {
		existing["theme"] = "dark"
		changed = true
	}

	// Ensure project trust (same pattern as Claude).
	if projectPath != "" {
		if ensureProjectTrust(existing, projectPath) {
			changed = true
		}
	}

	if !changed {
		if app != nil {
			app.log("[kode-onboarding] config already complete, no changes needed")
		}
		return nil
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	if app != nil {
		app.log(fmt.Sprintf("[kode-onboarding] updated %s with onboarding flags", configPath))
	}
	return nil
}
