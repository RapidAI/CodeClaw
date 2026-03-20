package main

import (
	"strings"
	"testing"
)

func TestSearchFilesInProject_EmptyPath(t *testing.T) {
	result := searchFilesInProject("", "pattern", "")
	if !strings.Contains(result, "未指定") {
		t.Errorf("expected '未指定' message, got %q", result)
	}
}

func TestSearchFilesInProject_NoMatch(t *testing.T) {
	dir := t.TempDir()
	result := searchFilesInProject(dir, "nonexistent_pattern_xyz", "")
	if strings.Contains(result, "error") {
		t.Errorf("unexpected error: %s", result)
	}
}

func TestCheckProjectHealth_EmptyPath(t *testing.T) {
	result := checkProjectHealth("")
	if !strings.Contains(result, "未指定") {
		t.Errorf("expected '未指定' message, got %q", result)
	}
}

func TestCheckProjectHealth_NoProject(t *testing.T) {
	dir := t.TempDir()
	result := checkProjectHealth(dir)
	if !strings.Contains(result, "未检测到") {
		t.Errorf("expected '未检测到' message, got %q", result)
	}
}

func TestRunGitCmd_InvalidDir(t *testing.T) {
	_, err := runGitCmd("/nonexistent_dir_xyz", "status")
	if err == nil {
		t.Error("expected error for invalid dir")
	}
}
