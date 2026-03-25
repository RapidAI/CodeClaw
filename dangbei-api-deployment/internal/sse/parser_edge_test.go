package sse

import "testing"

// ─── ParseDeepSeekSSELine edge cases ─────────────────────────────────

func TestParseDeepSeekSSELineEmptyLine(t *testing.T) {
	_, _, ok := ParseDeepSeekSSELine([]byte(""))
	if ok {
		t.Fatal("expected not parsed for empty line")
	}
}

func TestParseDeepSeekSSELineNoDataPrefix(t *testing.T) {
	_, _, ok := ParseDeepSeekSSELine([]byte("event: message"))
	if ok {
		t.Fatal("expected not parsed for non-data line")
	}
}

func TestParseDeepSeekSSELineInvalidJSON(t *testing.T) {
	_, _, ok := ParseDeepSeekSSELine([]byte("data: {invalid json"))
	if ok {
		t.Fatal("expected not parsed for invalid JSON")
	}
}

func TestParseDeepSeekSSELineWhitespaceOnly(t *testing.T) {
	_, _, ok := ParseDeepSeekSSELine([]byte("   "))
	if ok {
		t.Fatal("expected not parsed for whitespace-only line")
	}
}

func TestParseDeepSeekSSELineDataWithExtraSpaces(t *testing.T) {
	chunk, done, ok := ParseDeepSeekSSELine([]byte(`data:   {"v":"hello"}  `))
	if !ok || done {
		t.Fatalf("expected parsed chunk for spaced data line")
	}
	if chunk["v"] != "hello" {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
}

// ─── shouldSkipPath edge cases ───────────────────────────────────────

func TestShouldSkipPathQuasiStatus(t *testing.T) {
	if !shouldSkipPath("response/quasi_status") {
		t.Fatal("expected skip for quasi_status path")
	}
}

func TestShouldSkipPathElapsedSecs(t *testing.T) {
	if !shouldSkipPath("response/elapsed_secs") {
		t.Fatal("expected skip for elapsed_secs path")
	}
}

func TestShouldSkipPathTokenUsage(t *testing.T) {
	if !shouldSkipPath("response/token_usage") {
		t.Fatal("expected skip for token_usage path")
	}
}

func TestShouldSkipPathPendingFragment(t *testing.T) {
	if !shouldSkipPath("response/pending_fragment") {
		t.Fatal("expected skip for pending_fragment path")
	}
}

func TestShouldSkipPathConversationMode(t *testing.T) {
	if !shouldSkipPath("response/conversation_mode") {
		t.Fatal("expected skip for conversation_mode path")
	}
}

func TestShouldSkipPathSearchStatus(t *testing.T) {
	if !shouldSkipPath("response/search_status") {
		t.Fatal("expected skip for search_status path")
	}
}

func TestShouldSkipPathFragmentStatus(t *testing.T) {
	if !shouldSkipPath("response/fragments/-1/status") {
		t.Fatal("expected skip for fragment -1 status")
	}
	if !shouldSkipPath("response/fragments/-2/status") {
		t.Fatal("expected skip for fragment -2 status")
	}
	if !shouldSkipPath("response/fragments/-3/status") {
		t.Fatal("expected skip for fragment -3 status")
	}
}

func TestShouldSkipPathRegularContent(t *testing.T) {
	if shouldSkipPath("response/content") {
		t.Fatal("expected not skip for content path")
	}
	if shouldSkipPath("response/thinking_content") {
		t.Fatal("expected not skip for thinking_content path")
	}
}

// ─── ParseSSEChunkForContent edge cases ──────────────────────────────

func TestParseSSEChunkForContentNoVField(t *testing.T) {
	parts, finished, nextType := ParseSSEChunkForContent(map[string]any{"p": "response/content"}, false, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts when v is missing, got %#v", parts)
	}
	if nextType != "text" {
		t.Fatalf("expected type preserved, got %q", nextType)
	}
}

func TestParseSSEChunkForContentSkippedPath(t *testing.T) {
	parts, finished, nextType := ParseSSEChunkForContent(map[string]any{
		"p": "response/token_usage",
		"v": "some data",
	}, false, "text")
	if finished || len(parts) > 0 {
		t.Fatalf("expected skipped path to produce no output")
	}
	if nextType != "text" {
		t.Fatalf("expected type preserved for skipped path")
	}
}

func TestParseSSEChunkForContentFinishedStatus(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{
		"p": "response/status",
		"v": "FINISHED",
	}, false, "text")
	if !finished {
		t.Fatal("expected finished on status FINISHED")
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts on finished, got %d", len(parts))
	}
}

func TestParseSSEChunkForContentStatusNotFinished(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{
		"p": "response/status",
		"v": "IN_PROGRESS",
	}, false, "text")
	if finished {
		t.Fatal("expected not finished for non-FINISHED status")
	}
	if len(parts) != 1 || parts[0].Text != "IN_PROGRESS" {
		t.Fatalf("expected content for non-FINISHED status, got %#v", parts)
	}
}

func TestParseSSEChunkForContentEmptyStringV(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{
		"p": "response/content",
		"v": "",
	}, false, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts for empty string v, got %#v", parts)
	}
}

func TestParseSSEChunkForContentFinishedOnEmptyPath(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{
		"p": "",
		"v": "FINISHED",
	}, false, "text")
	if !finished {
		t.Fatal("expected finished on empty path with FINISHED value")
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts on finished")
	}
}

func TestParseSSEChunkForContentFinishedOnStatusPath(t *testing.T) {
	_, finished, _ := ParseSSEChunkForContent(map[string]any{
		"p": "status",
		"v": "FINISHED",
	}, false, "text")
	if !finished {
		t.Fatal("expected finished on status path with FINISHED value")
	}
}

func TestParseSSEChunkForContentThinkingPathEmptyPath(t *testing.T) {
	parts, _, nextType := ParseSSEChunkForContent(map[string]any{
		"v": "some thought",
	}, true, "thinking")
	if len(parts) != 1 || parts[0].Type != "thinking" {
		t.Fatalf("expected thinking part on empty path, got %#v", parts)
	}
	if nextType != "thinking" {
		t.Fatalf("expected nextType thinking, got %q", nextType)
	}
}

func TestParseSSEChunkForContentThinkingEnabledTextType(t *testing.T) {
	parts, _, nextType := ParseSSEChunkForContent(map[string]any{
		"v": "text content",
	}, true, "text")
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected text part when currentType=text, got %#v", parts)
	}
	if nextType != "text" {
		t.Fatalf("expected nextType text, got %q", nextType)
	}
}

// ─── ParseSSEChunkForContent: fragments path with THINK type ─────────

func TestParseSSEChunkForContentFragmentsAppendThink(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{
				"type":    "THINK",
				"content": "深入思考...",
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	if nextType != "thinking" {
		t.Fatalf("expected nextType thinking, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "thinking" || parts[0].Text != "深入思考..." {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentFragmentsAppendEmptyContent(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{
				"type":    "RESPONSE",
				"content": "",
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "thinking")
	if finished {
		t.Fatal("expected not finished")
	}
	if nextType != "text" {
		t.Fatalf("expected nextType text, got %q", nextType)
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts for empty content, got %#v", parts)
	}
}

func TestParseSSEChunkForContentFragmentsAppendDefaultType(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{
				"type":    "UNKNOWN",
				"content": "some text",
			},
		},
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, true, "text")
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected text type for unknown fragment type, got %#v", parts)
	}
}

func TestParseSSEChunkForContentFragmentsAppendNonArray(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": "not an array",
	}
	parts, finished, _ := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	// "not an array" should be treated as string value at the end
	if len(parts) != 1 || parts[0].Text != "not an array" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentFragmentsAppendNonMap(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{"string item"},
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, false, "text")
	// Non-map items in fragment array are skipped; the []any itself is handled later
	_ = parts // just checking it doesn't panic
}

// ─── ParseSSEChunkForContent: response path with nested fragment ─────

func TestParseSSEChunkForContentResponsePathFragmentsAppend(t *testing.T) {
	chunk := map[string]any{
		"p": "response",
		"v": []any{
			map[string]any{
				"p": "fragments",
				"o": "APPEND",
				"v": []any{
					map[string]any{
						"type": "THINKING",
					},
				},
			},
		},
	}
	_, _, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if nextType != "thinking" {
		t.Fatalf("expected nextType thinking from response path fragments, got %q", nextType)
	}
}

func TestParseSSEChunkForContentResponsePathResponseFragment(t *testing.T) {
	chunk := map[string]any{
		"p": "response",
		"v": []any{
			map[string]any{
				"p": "fragments",
				"o": "APPEND",
				"v": []any{
					map[string]any{
						"type": "RESPONSE",
					},
				},
			},
		},
	}
	_, _, nextType := ParseSSEChunkForContent(chunk, true, "thinking")
	if nextType != "text" {
		t.Fatalf("expected nextType text for RESPONSE fragment, got %q", nextType)
	}
}

// ─── ParseSSEChunkForContent: map value with wrapped response ────────

func TestParseSSEChunkForContentMapValueWithFragments(t *testing.T) {
	chunk := map[string]any{
		"v": map[string]any{
			"response": map[string]any{
				"fragments": []any{
					map[string]any{
						"type":    "THINK",
						"content": "思考...",
					},
					map[string]any{
						"type":    "RESPONSE",
						"content": "回答...",
					},
				},
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	if nextType != "text" {
		t.Fatalf("expected nextType text after RESPONSE, got %q", nextType)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %#v", len(parts), parts)
	}
	if parts[0].Type != "thinking" || parts[0].Text != "思考..." {
		t.Fatalf("first part mismatch: %#v", parts[0])
	}
	if parts[1].Type != "text" || parts[1].Text != "回答..." {
		t.Fatalf("second part mismatch: %#v", parts[1])
	}
}

func TestParseSSEChunkForContentMapValueDirectFragments(t *testing.T) {
	chunk := map[string]any{
		"v": map[string]any{
			"fragments": []any{
				map[string]any{
					"type":    "RESPONSE",
					"content": "直接回答",
				},
			},
		},
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, false, "text")
	if len(parts) != 1 || parts[0].Text != "直接回答" || parts[0].Type != "text" {
		t.Fatalf("unexpected parts for direct fragments: %#v", parts)
	}
}

func TestParseSSEChunkForContentMapValueUnknownType(t *testing.T) {
	chunk := map[string]any{
		"v": map[string]any{
			"fragments": []any{
				map[string]any{
					"type":    "CUSTOM",
					"content": "custom content",
				},
			},
		},
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, false, "text")
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected partType fallback for unknown type, got %#v", parts)
	}
}

func TestParseSSEChunkForContentMapValueEmptyFragmentContent(t *testing.T) {
	chunk := map[string]any{
		"v": map[string]any{
			"fragments": []any{
				map[string]any{
					"type":    "RESPONSE",
					"content": "",
				},
			},
		},
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, false, "text")
	if len(parts) != 0 {
		t.Fatalf("expected no parts for empty fragment content, got %#v", parts)
	}
}

// ─── ParseSSEChunkForContent: fragments/-1/content path ──────────────

func TestParseSSEChunkForContentFragmentContentPathInheritsType(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments/-1/content",
		"v": "继续思考",
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, true, "thinking")
	if len(parts) != 1 || parts[0].Type != "thinking" {
		t.Fatalf("expected inherited thinking type, got %#v", parts)
	}
}

// ─── IsCitation edge cases ───────────────────────────────────────────

func TestIsCitationWithLeadingWhitespace(t *testing.T) {
	if !IsCitation("   [citation:2] text") {
		t.Fatal("expected citation true with leading whitespace")
	}
}

func TestIsCitationEmpty(t *testing.T) {
	if IsCitation("") {
		t.Fatal("expected citation false for empty string")
	}
}

func TestIsCitationSimilarPrefix(t *testing.T) {
	if IsCitation("[cite:1] text") {
		t.Fatal("expected citation false for [cite: prefix")
	}
}

// ─── extractContentRecursive edge cases ──────────────────────────────

func TestExtractContentRecursiveFinishedStatus(t *testing.T) {
	items := []any{
		map[string]any{"p": "status", "v": "FINISHED"},
	}
	parts, finished := extractContentRecursive(items, "text")
	if !finished {
		t.Fatal("expected finished on status FINISHED")
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts, got %#v", parts)
	}
}

func TestExtractContentRecursiveSkipsPath(t *testing.T) {
	items := []any{
		map[string]any{"p": "token_usage", "v": "data"},
	}
	parts, finished := extractContentRecursive(items, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	if len(parts) != 0 {
		t.Fatalf("expected no parts for skipped path, got %#v", parts)
	}
}

func TestExtractContentRecursiveContentField(t *testing.T) {
	items := []any{
		map[string]any{"p": "x", "v": "val", "content": "actual content", "type": "RESPONSE"},
	}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 1 || parts[0].Text != "actual content" || parts[0].Type != "text" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestExtractContentRecursiveContentFieldThinkType(t *testing.T) {
	items := []any{
		map[string]any{"p": "x", "v": "val", "content": "think text", "type": "THINK"},
	}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 1 || parts[0].Type != "thinking" {
		t.Fatalf("expected thinking type for THINK content, got %#v", parts)
	}
}

func TestExtractContentRecursiveThinkingPath(t *testing.T) {
	items := []any{
		map[string]any{"p": "thinking_content", "v": "deep thought"},
	}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 1 || parts[0].Type != "thinking" || parts[0].Text != "deep thought" {
		t.Fatalf("unexpected parts for thinking path: %#v", parts)
	}
}

func TestExtractContentRecursiveContentPath(t *testing.T) {
	items := []any{
		map[string]any{"p": "content", "v": "text content"},
	}
	parts, _ := extractContentRecursive(items, "thinking")
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected text type for content path, got %#v", parts)
	}
}

func TestExtractContentRecursiveResponsePath(t *testing.T) {
	items := []any{
		map[string]any{"p": "response", "v": "text content"},
	}
	parts, _ := extractContentRecursive(items, "thinking")
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected text type for response path, got %#v", parts)
	}
}

func TestExtractContentRecursiveFragmentsPath(t *testing.T) {
	items := []any{
		map[string]any{"p": "fragments", "v": "fragment text"},
	}
	parts, _ := extractContentRecursive(items, "thinking")
	if len(parts) != 1 || parts[0].Type != "text" {
		t.Fatalf("expected text type for fragments path, got %#v", parts)
	}
}

func TestExtractContentRecursiveNestedArrayWithTypes(t *testing.T) {
	items := []any{
		map[string]any{
			"p": "fragments",
			"v": []any{
				map[string]any{"content": "thought", "type": "THINKING"},
				map[string]any{"content": "answer", "type": "RESPONSE"},
				"raw string",
			},
		},
	}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %#v", len(parts), parts)
	}
	if parts[0].Type != "thinking" || parts[0].Text != "thought" {
		t.Fatalf("first part mismatch: %#v", parts[0])
	}
	if parts[1].Type != "text" || parts[1].Text != "answer" {
		t.Fatalf("second part mismatch: %#v", parts[1])
	}
	if parts[2].Type != "text" || parts[2].Text != "raw string" {
		t.Fatalf("third part mismatch: %#v", parts[2])
	}
}

func TestExtractContentRecursiveEmptyContentSkipped(t *testing.T) {
	items := []any{
		map[string]any{
			"p": "fragments",
			"v": []any{
				map[string]any{"content": "", "type": "RESPONSE"},
			},
		},
	}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 0 {
		t.Fatalf("expected no parts for empty nested content, got %#v", parts)
	}
}

func TestExtractContentRecursiveFinishedString(t *testing.T) {
	items := []any{
		map[string]any{"p": "content", "v": "FINISHED"},
	}
	parts, _ := extractContentRecursive(items, "text")
	// "FINISHED" string value on non-status path should be skipped
	if len(parts) != 0 {
		t.Fatalf("expected FINISHED string to be skipped, got %#v", parts)
	}
}

func TestExtractContentRecursiveNoVField(t *testing.T) {
	items := []any{
		map[string]any{"p": "content"},
	}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 0 {
		t.Fatalf("expected no parts for missing v field, got %#v", parts)
	}
}

func TestExtractContentRecursiveNonMapItem(t *testing.T) {
	items := []any{"just a string", 42}
	parts, _ := extractContentRecursive(items, "text")
	if len(parts) != 0 {
		t.Fatalf("expected no parts for non-map items, got %#v", parts)
	}
}
