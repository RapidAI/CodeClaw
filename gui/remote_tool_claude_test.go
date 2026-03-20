package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeAdapterResolveClaudeExecutablePrefersExeOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path resolution")
	}

	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "claude.cmd")
	exePath := filepath.Join(dir, "claude.exe")

	if err := os.WriteFile(cmdPath, []byte("@echo off"), 0o644); err != nil {
		t.Fatalf("WriteFile(cmd) error = %v", err)
	}
	if err := os.WriteFile(exePath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(exe) error = %v", err)
	}

	adapter := NewClaudeAdapter(&App{})
	got := adapter.resolveClaudeExecutable(cmdPath)
	if got != exePath {
		t.Fatalf("resolveClaudeExecutable() = %q, want %q", got, exePath)
	}
}

func TestClaudeAdapterBuildCommandEnvPreservesBaseEntries(t *testing.T) {
	t.Setenv("PATH", `C:\Windows\System32`)
	t.Setenv("AppData", `C:\Users\tester\AppData\Roaming`)
	t.Setenv("USERPROFILE", `C:\Users\tester`)
	t.Setenv("HOME", `C:\Users\tester`)

	adapter := NewClaudeAdapter(&App{})
	env := adapter.buildCommandEnv(map[string]string{
		"ANTHROPIC_MODEL": "claude-test",
		"PATH":            `D:\custom\bin`,
	})

	if env["ANTHROPIC_MODEL"] != "claude-test" {
		t.Fatalf("ANTHROPIC_MODEL = %q, want %q", env["ANTHROPIC_MODEL"], "claude-test")
	}
	if env["CLAUDE_CODE_USE_COLORS"] != "true" {
		t.Fatalf("CLAUDE_CODE_USE_COLORS = %q, want %q", env["CLAUDE_CODE_USE_COLORS"], "true")
	}
	if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "64000" {
		t.Fatalf("CLAUDE_CODE_MAX_OUTPUT_TOKENS = %q, want %q", env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"], "64000")
	}
	if !strings.Contains(env["PATH"], `D:\custom\bin`) {
		t.Fatalf("PATH %q should include custom base PATH", env["PATH"])
	}
	if !strings.Contains(env["PATH"], `C:\Program Files\nodejs`) {
		t.Fatalf("PATH %q should include Node.js path", env["PATH"])
	}
}
