package sse

import "testing"

func TestParseDeepSeekSSELine(t *testing.T) {
	chunk, done, ok := ParseDeepSeekSSELine([]byte(`data: {"v":"你好"}`))
	if !ok || done {
		t.Fatalf("expected parsed chunk")
	}
	if chunk["v"] != "你好" {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
}

func TestParseDeepSeekSSELineDone(t *testing.T) {
	_, done, ok := ParseDeepSeekSSELine([]byte(`data: [DONE]`))
	if !ok || !done {
		t.Fatalf("expected done signal")
	}
}

func TestParseSSEChunkForContentSimple(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{"v": "hello"}, false, "text")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Text != "hello" || parts[0].Type != "text" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentThinking(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{"p": "response/thinking_content", "v": "think"}, true, "thinking")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Type != "thinking" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestIsCitation(t *testing.T) {
	if !IsCitation("[citation:1] abc") {
		t.Fatal("expected citation true")
	}
	if IsCitation("normal text") {
		t.Fatal("expected citation false")
	}
}

func TestParseSSEChunkForContentFragmentsAppendSwitchToResponse(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{
				"type":    "RESPONSE",
				"content": "你好",
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "thinking")
	if finished {
		t.Fatal("expected unfinished")
	}
	if nextType != "text" {
		t.Fatalf("expected next type text, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "你好" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentAfterAppendUsesUpdatedType(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments/-1/content",
		"v": "！",
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected unfinished")
	}
	if nextType != "text" {
		t.Fatalf("expected next type text, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "！" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}
