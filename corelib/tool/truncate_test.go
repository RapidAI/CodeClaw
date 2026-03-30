package tool

import (
	"strings"
	"testing"
)

func TestTruncateBody_Empty(t *testing.T) {
	if got := TruncateBody("", 100); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := TruncateBody("hello", 0); got != "" {
		t.Errorf("expected empty for maxChars=0, got %q", got)
	}
	if got := TruncateBody("hello", -1); got != "" {
		t.Errorf("expected empty for maxChars=-1, got %q", got)
	}
}

func TestTruncateBody_ExactLimit(t *testing.T) {
	body := "abcde"
	if got := TruncateBody(body, 5); got != body {
		t.Errorf("expected %q, got %q", body, got)
	}
	// Multi-line exactly at limit.
	body2 := "ab\ncd" // 5 runes
	if got := TruncateBody(body2, 5); got != body2 {
		t.Errorf("expected %q, got %q", body2, got)
	}
}

func TestTruncateBody_PreservesHeadings(t *testing.T) {
	body := "# Title\n- item1\n- item2\nsome long paragraph that pushes us over the limit easily"
	result := TruncateBody(body, 30)
	if !strings.Contains(result, "# Title") {
		t.Errorf("heading should be preserved, got %q", result)
	}
	if !strings.Contains(result, "- item1") {
		t.Errorf("list item should be preserved, got %q", result)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("should end with '...', got %q", result)
	}
}

func TestTruncateBody_CodeBlockBoundary(t *testing.T) {
	body := "```go\nfunc main() {}\n```\nextra content that exceeds the limit"
	result := TruncateBody(body, 30)
	// Should preserve complete lines up to the budget.
	if !strings.Contains(result, "```go") {
		t.Errorf("code block start should be preserved, got %q", result)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("should end with '...', got %q", result)
	}
}

func TestTruncateBody_SingleOverlongLine(t *testing.T) {
	body := strings.Repeat("x", 2000)
	result := TruncateBody(body, 100)
	if !strings.HasSuffix(result, "...") {
		t.Errorf("should end with '...', got suffix %q", result[len(result)-10:])
	}
	// The rune content before "..." should be exactly 100 chars.
	trimmed := strings.TrimSuffix(result, "...")
	if len([]rune(trimmed)) != 100 {
		t.Errorf("expected 100 runes before '...', got %d", len([]rune(trimmed)))
	}
}
