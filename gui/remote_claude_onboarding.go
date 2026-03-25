package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// ensureClaudeOnboardingComplete delegates to the shared corelib
// implementation that writes hasCompletedOnboarding, bypassPermissionsModeAccepted,
// theme, project trust, and custom API key approval to ~/.claude.json.
func ensureClaudeOnboardingComplete(app *App, projectPath string, apiKey ...string) error {
	key := ""
	if len(apiKey) > 0 {
		key = apiKey[0]
	}
	logFn := func(msg string) {
		if app != nil {
			app.log(msg)
		}
	}
	return configfile.EnsureClaudeOnboarding(configfile.ClaudeOnboardingOptions{
		ConfigFileName: ".claude.json",
		LogTag:         "claude",
		ProjectPath:    projectPath,
		ApiKey:         key,
	}, logFn)
}

// ensureProjectTrust delegates to the corelib implementation.
func ensureProjectTrust(config map[string]any, projectPath string) bool {
	return configfile.EnsureProjectTrust(config, projectPath)
}

// isTruthy is a local wrapper around configfile's unexported isTruthy.
// Kept for backward compatibility with callers and tests in gui/.
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
