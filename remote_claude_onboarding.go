package main

import (
	"path/filepath"
	"strings"
)

// ensureClaudeOnboardingComplete checks that Claude Code's user-level
// config file (~/.claude.json) contains the flags that mark onboarding
// as finished.  If the file is missing or the flags are absent, they
// are added so that Claude Code skips the interactive first-run wizard
// when launched in a remote PTY session.
//
// It also ensures the given projectPath has a trust entry so Claude
// Code doesn't prompt "Do you trust this project folder?" on launch.
//
// This is necessary because:
//   - Remote sessions may run under a user profile where Claude Code
//     has never been launched interactively.
//   - Remote sessions may use git worktree paths that Claude Code has
//     never seen before.
//   - The onboarding wizard and trust dialog are TUI flows that require
//     manual input which is hard to automate through a PTY relay.
//
// The function is idempotent — it only adds missing keys and never
// removes existing user preferences.
func ensureClaudeOnboardingComplete(app *App, projectPath string) error {
	return ensureClaudeCodeForkOnboarding(app, ".claude.json", "claude", projectPath)
}

// ensureProjectTrust adds a trust entry for the given project path in
// the "projects" map of ~/.claude.json.  Returns true if the config
// was modified.
func ensureProjectTrust(config map[string]any, projectPath string) bool {
	// Normalize the path to use forward slashes (Claude Code on Windows
	// stores paths with forward slashes in its config).
	normalizedPath := filepath.ToSlash(filepath.Clean(projectPath))

	projects, ok := config["projects"].(map[string]any)
	if !ok {
		projects = map[string]any{}
		config["projects"] = projects
	}

	// Check if this exact path or a variant already has trust.
	for key, val := range projects {
		// Normalize stored key for comparison.
		normalizedKey := filepath.ToSlash(filepath.Clean(key))
		if normalizedKey == normalizedPath {
			entry, ok := val.(map[string]any)
			if ok && isTruthy(entry["hasTrustDialogAccepted"]) {
				return false // Already trusted
			}
			// Entry exists but trust not set — update it.
			if entry == nil {
				entry = map[string]any{}
			}
			entry["hasTrustDialogAccepted"] = true
			projects[key] = entry
			return true
		}
	}

	// No entry for this path — create one with minimal trust flags.
	projects[normalizedPath] = map[string]any{
		"allowedTools":          []any{},
		"hasTrustDialogAccepted": true,
	}
	return true
}

// isTruthy checks if a JSON value is boolean true or the string "true".
func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	default:
		return false
	}
}
