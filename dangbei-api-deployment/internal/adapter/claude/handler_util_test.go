package claude

import (
	"testing"
)

// ─── normalizeClaudeMessages ─────────────────────────────────────────

func TestNormalizeClaudeMessagesSimpleString(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "Hello"},
	}
	got := normalizeClaudeMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	m := got[0].(map[string]any)
	if m["content"] != "Hello" {
		t.Fatalf("expected 'Hello', got %v", m["content"])
	}
}

func TestNormalizeClaudeMessagesArrayContent(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "line1"},
				map[string]any{"type": "text", "text": "line2"},
			},
		},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	if m["content"] != "line1\nline2" {
		t.Fatalf("expected joined text, got %q", m["content"])
	}
}

func TestNormalizeClaudeMessagesToolResult(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "tool_result", "content": "tool output"},
			},
		},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	if m["content"] != "tool output" {
		t.Fatalf("expected 'tool output', got %q", m["content"])
	}
}

func TestNormalizeClaudeMessagesSkipsNonMap(t *testing.T) {
	msgs := []any{"not a map", 42}
	got := normalizeClaudeMessages(msgs)
	if len(got) != 0 {
		t.Fatalf("expected 0 messages for non-map items, got %d", len(got))
	}
}

func TestNormalizeClaudeMessagesEmpty(t *testing.T) {
	got := normalizeClaudeMessages(nil)
	if len(got) != 0 {
		t.Fatalf("expected 0, got %d", len(got))
	}
}

func TestNormalizeClaudeMessagesPreservesRole(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "assistant", "content": "response"},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	if m["role"] != "assistant" {
		t.Fatalf("expected 'assistant', got %q", m["role"])
	}
}

func TestNormalizeClaudeMessagesMixedContentBlocks(t *testing.T) {
	msgs := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "Hello"},
				map[string]any{"type": "image", "source": "data:..."},
				map[string]any{"type": "text", "text": "World"},
			},
		},
	}
	got := normalizeClaudeMessages(msgs)
	m := got[0].(map[string]any)
	if m["content"] != "Hello\nWorld" {
		t.Fatalf("expected only text parts joined, got %q", m["content"])
	}
}

// ─── buildClaudeToolPrompt ───────────────────────────────────────────

func TestBuildClaudeToolPromptSingleTool(t *testing.T) {
	tools := []any{
		map[string]any{
			"name":        "search",
			"description": "Search the web",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
		},
	}
	prompt := buildClaudeToolPrompt(tools)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	// Should contain tool name and description
	if !containsStr(prompt, "search") {
		t.Fatalf("expected 'search' in prompt")
	}
	if !containsStr(prompt, "Search the web") {
		t.Fatalf("expected description in prompt")
	}
	if !containsStr(prompt, "tool_calls") {
		t.Fatalf("expected tool_calls instruction in prompt")
	}
}

func TestBuildClaudeToolPromptMultipleTools(t *testing.T) {
	tools := []any{
		map[string]any{"name": "tool1", "description": "desc1"},
		map[string]any{"name": "tool2", "description": "desc2"},
	}
	prompt := buildClaudeToolPrompt(tools)
	if !containsStr(prompt, "tool1") || !containsStr(prompt, "tool2") {
		t.Fatalf("expected both tools in prompt")
	}
}

func TestBuildClaudeToolPromptSkipsNonMap(t *testing.T) {
	tools := []any{"not a map"}
	prompt := buildClaudeToolPrompt(tools)
	if prompt == "" {
		t.Fatal("expected non-empty prompt even with invalid tools")
	}
	// Should still contain the intro and instruction
	if !containsStr(prompt, "You are Claude") {
		t.Fatalf("expected intro in prompt")
	}
}

// ─── hasSystemMessage ────────────────────────────────────────────────

func TestHasSystemMessageTrue(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "system", "content": "You are a helper"},
		map[string]any{"role": "user", "content": "Hi"},
	}
	if !hasSystemMessage(msgs) {
		t.Fatal("expected true")
	}
}

func TestHasSystemMessageFalse(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "Hi"},
		map[string]any{"role": "assistant", "content": "Hello"},
	}
	if hasSystemMessage(msgs) {
		t.Fatal("expected false")
	}
}

func TestHasSystemMessageEmpty(t *testing.T) {
	if hasSystemMessage(nil) {
		t.Fatal("expected false for nil")
	}
}

func TestHasSystemMessageNonMap(t *testing.T) {
	msgs := []any{"not a map"}
	if hasSystemMessage(msgs) {
		t.Fatal("expected false for non-map")
	}
}

// ─── extractClaudeToolNames ──────────────────────────────────────────

func TestExtractClaudeToolNamesSingle(t *testing.T) {
	tools := []any{
		map[string]any{"name": "search"},
	}
	names := extractClaudeToolNames(tools)
	if len(names) != 1 || names[0] != "search" {
		t.Fatalf("expected [search], got %v", names)
	}
}

func TestExtractClaudeToolNamesMultiple(t *testing.T) {
	tools := []any{
		map[string]any{"name": "search"},
		map[string]any{"name": "calculate"},
	}
	names := extractClaudeToolNames(tools)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %v", names)
	}
}

func TestExtractClaudeToolNamesSkipsEmptyName(t *testing.T) {
	tools := []any{
		map[string]any{"name": ""},
		map[string]any{"name": "valid"},
	}
	names := extractClaudeToolNames(tools)
	if len(names) != 1 || names[0] != "valid" {
		t.Fatalf("expected [valid], got %v", names)
	}
}

func TestExtractClaudeToolNamesSkipsNonMap(t *testing.T) {
	tools := []any{"not a map", 42}
	names := extractClaudeToolNames(tools)
	if len(names) != 0 {
		t.Fatalf("expected 0, got %v", names)
	}
}

func TestExtractClaudeToolNamesNil(t *testing.T) {
	names := extractClaudeToolNames(nil)
	if len(names) != 0 {
		t.Fatalf("expected 0, got %v", names)
	}
}

// ─── toMessageMaps ───────────────────────────────────────────────────

func TestToMessageMapsNormal(t *testing.T) {
	input := []any{
		map[string]any{"role": "user", "content": "Hello"},
	}
	got := toMessageMaps(input)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
}

func TestToMessageMapsNonSlice(t *testing.T) {
	got := toMessageMaps("not a slice")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestToMessageMapsSkipsNonMap(t *testing.T) {
	input := []any{"string", map[string]any{"role": "user"}, 42}
	got := toMessageMaps(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 map, got %d", len(got))
	}
}

func TestToMessageMapsNil(t *testing.T) {
	got := toMessageMaps(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// ─── extractMessageContent ──────────────────────────────────────────

func TestExtractMessageContentString(t *testing.T) {
	if got := extractMessageContent("hello"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestExtractMessageContentArray(t *testing.T) {
	input := []any{"part1", "part2"}
	got := extractMessageContent(input)
	if got != "part1\npart2" {
		t.Fatalf("expected joined, got %q", got)
	}
}

func TestExtractMessageContentOther(t *testing.T) {
	got := extractMessageContent(42)
	if got != "42" {
		t.Fatalf("expected '42', got %q", got)
	}
}

func TestExtractMessageContentNil(t *testing.T) {
	got := extractMessageContent(nil)
	if got != "<nil>" {
		t.Fatalf("expected '<nil>', got %q", got)
	}
}

// ─── cloneMap ────────────────────────────────────────────────────────

func TestCloneMapBasic(t *testing.T) {
	original := map[string]any{"a": 1, "b": "hello"}
	clone := cloneMap(original)
	original["a"] = 999
	if clone["a"] != 1 {
		t.Fatalf("expected 1, got %v", clone["a"])
	}
	if clone["b"] != "hello" {
		t.Fatalf("expected 'hello', got %v", clone["b"])
	}
}

func TestCloneMapEmpty(t *testing.T) {
	clone := cloneMap(map[string]any{})
	if len(clone) != 0 {
		t.Fatalf("expected empty, got %v", clone)
	}
}

func TestCloneMapNested(t *testing.T) {
	// cloneMap is shallow, so nested maps share references
	inner := map[string]any{"key": "value"}
	original := map[string]any{"nested": inner}
	clone := cloneMap(original)
	// Shallow clone means inner is shared
	inner["key"] = "modified"
	cloneNested := clone["nested"].(map[string]any)
	if cloneNested["key"] != "modified" {
		t.Fatal("expected shallow clone to share nested references")
	}
}

// helper
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
