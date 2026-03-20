package remote

import (
	"path/filepath"
	"strings"
)

// EnsureProjectTrust adds a trust entry for the given project path in
// the "projects" map of a Claude Code config. Returns true if modified.
func EnsureProjectTrust(config map[string]any, projectPath string) bool {
	normalizedPath := filepath.ToSlash(filepath.Clean(projectPath))

	projects, ok := config["projects"].(map[string]any)
	if !ok {
		projects = map[string]any{}
		config["projects"] = projects
	}

	for key, val := range projects {
		normalizedKey := filepath.ToSlash(filepath.Clean(key))
		if normalizedKey == normalizedPath {
			entry, ok := val.(map[string]any)
			if ok && IsTruthy(entry["hasTrustDialogAccepted"]) {
				return false
			}
			if entry == nil {
				entry = map[string]any{}
			}
			entry["hasTrustDialogAccepted"] = true
			projects[key] = entry
			return true
		}
	}

	projects[normalizedPath] = map[string]any{
		"allowedTools":            []any{},
		"hasTrustDialogAccepted": true,
	}
	return true
}

// IsTruthy checks if a JSON value is boolean true or the string "true".
func IsTruthy(v any) bool {
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
