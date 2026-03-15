//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGeminiOnboardingCreatesSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".gemini", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("settings file not created: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	ui, ok := config["ui"].(map[string]any)
	if !ok {
		t.Fatal("ui section missing")
	}
	if ui["theme"] != "Default Dark" {
		t.Errorf("theme = %v, want Default Dark", ui["theme"])
	}
}

func TestEnsureGeminiOnboardingPreservesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{
		"ui": map[string]any{
			"theme":    "Solarized",
			"hideTips": true,
		},
		"general": map[string]any{
			"vimMode": true,
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(updated, &config)

	ui := config["ui"].(map[string]any)
	if ui["theme"] != "Solarized" {
		t.Errorf("theme was overwritten: got %v, want Solarized", ui["theme"])
	}
	if ui["hideTips"] != true {
		t.Error("hideTips was lost")
	}

	general := config["general"].(map[string]any)
	if general["vimMode"] != true {
		t.Error("general.vimMode was lost")
	}
}

func TestEnsureGeminiOnboardingIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{
		"ui": map[string]any{
			"theme": "GitHub",
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	beforeStat, _ := os.Stat(configPath)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterStat, _ := os.Stat(configPath)
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Error("file was rewritten even though no changes were needed")
	}
}

func TestEnsureGeminiOnboardingHandlesCorruptFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")
	os.WriteFile(configPath, []byte("not valid json{{{"), 0o644)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backupPath := configPath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("corrupt file was not backed up")
	}

	data, _ := os.ReadFile(configPath)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("new config is not valid JSON: %v", err)
	}
	ui := config["ui"].(map[string]any)
	if ui["theme"] != "Default Dark" {
		t.Errorf("theme = %v, want Default Dark", ui["theme"])
	}
}

func TestEnsureKodeOnboardingCreatesConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, `D:\projects\myapp`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".kode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if !isTruthy(config["hasCompletedOnboarding"]) {
		t.Error("hasCompletedOnboarding should be true")
	}
	if config["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", config["theme"])
	}

	projects, ok := config["projects"].(map[string]any)
	if !ok {
		t.Fatal("projects map missing")
	}
	entry, ok := projects["D:/projects/myapp"].(map[string]any)
	if !ok {
		t.Fatal("project entry missing")
	}
	if !isTruthy(entry["hasTrustDialogAccepted"]) {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestEnsureKodeOnboardingPreservesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "light",
		"customKey":              "keep-me",
	}
	data, _ := json.Marshal(existing)
	os.WriteFile(configPath, data, 0o644)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(updated, &config)

	if config["theme"] != "light" {
		t.Errorf("theme was overwritten: got %v, want light", config["theme"])
	}
	if config["customKey"] != "keep-me" {
		t.Error("customKey was lost")
	}
}

func TestEnsureKodeOnboardingIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "dark",
		"projects": map[string]any{
			"D:/test": map[string]any{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	beforeStat, _ := os.Stat(configPath)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, `D:\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterStat, _ := os.Stat(configPath)
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Error("file was rewritten even though no changes were needed")
	}
}

func TestEnsureKodeOnboardingHandlesCorruptFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")
	os.WriteFile(configPath, []byte("not valid json{{{"), 0o644)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, `D:\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backupPath := configPath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("corrupt file was not backed up")
	}

	data, _ := os.ReadFile(configPath)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("new config is not valid JSON: %v", err)
	}
	if !isTruthy(config["hasCompletedOnboarding"]) {
		t.Error("hasCompletedOnboarding should be true")
	}
}

func TestEnsureToolOnboardingDispatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}

	// Should not panic for unknown tools.
	ensureToolOnboardingComplete(app, "unknown-tool", "/some/path")

	// Should handle claude.
	ensureToolOnboardingComplete(app, "claude", `D:\test`)
	if _, err := os.Stat(filepath.Join(tmpHome, ".claude.json")); os.IsNotExist(err) {
		t.Error("claude onboarding should have created .claude.json")
	}

	// Should handle gemini.
	ensureToolOnboardingComplete(app, "gemini", "")
	if _, err := os.Stat(filepath.Join(tmpHome, ".gemini", "settings.json")); os.IsNotExist(err) {
		t.Error("gemini onboarding should have created settings.json")
	}

	// Should handle kode.
	ensureToolOnboardingComplete(app, "kode", `D:\test`)
	if _, err := os.Stat(filepath.Join(tmpHome, ".kode.json")); os.IsNotExist(err) {
		t.Error("kode onboarding should have created .kode.json")
	}

	// Should be a no-op for tools without onboarding.
	ensureToolOnboardingComplete(app, "codex", "")
	ensureToolOnboardingComplete(app, "iflow", "")
	ensureToolOnboardingComplete(app, "kilo", "")
	ensureToolOnboardingComplete(app, "cursor", "")
}
