package openai

import (
	"strings"
	"testing"

	"ds2api/internal/util"
)

func TestNormalizeOpenAIMessagesForPrompt_AssistantToolCallsAndToolResult(t *testing.T) {
	raw := []any{
		map[string]any{"role": "system", "content": "You are helpful"},
		map[string]any{"role": "user", "content": "查北京天气"},
		map[string]any{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []any{
				map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": "{\"city\":\"beijing\"}",
					},
				},
			},
		},
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_1",
			"name":         "get_weather",
			"content":      "{\"temp\":18}",
		},
	}

	normalized := normalizeOpenAIMessagesForPrompt(raw)
	if len(normalized) != 4 {
		t.Fatalf("expected 4 normalized messages, got %d", len(normalized))
	}
	assistantContent, _ := normalized[2]["content"].(string)
	if !strings.Contains(assistantContent, "tool_call_id: call_1") ||
		!strings.Contains(assistantContent, "function.name: get_weather") ||
		!strings.Contains(assistantContent, "function.arguments: {\"city\":\"beijing\"}") {
		t.Fatalf("assistant tool call not serialized correctly: %q", assistantContent)
	}
	toolContent, _ := normalized[3]["content"].(string)
	if !strings.Contains(toolContent, "Tool result:") || !strings.Contains(toolContent, "name: get_weather") {
		t.Fatalf("tool result not serialized correctly: %q", toolContent)
	}

	prompt := util.MessagesPrepare(normalized)
	if !strings.Contains(prompt, "tool_call_id: call_1") || !strings.Contains(prompt, "Tool result:") {
		t.Fatalf("expected prompt to include tool call + result semantics: %q", prompt)
	}
}

func TestNormalizeOpenAIMessagesForPrompt_ToolObjectContentPreserved(t *testing.T) {
	raw := []any{
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_2",
			"name":         "get_weather",
			"content": map[string]any{
				"temp":      18,
				"condition": "sunny",
			},
		},
	}

	normalized := normalizeOpenAIMessagesForPrompt(raw)
	got, _ := normalized[0]["content"].(string)
	if !strings.Contains(got, `"temp":18`) || !strings.Contains(got, `"condition":"sunny"`) {
		t.Fatalf("expected serialized object in tool content, got %q", got)
	}
}

func TestNormalizeOpenAIMessagesForPrompt_ToolArrayBlocksJoined(t *testing.T) {
	raw := []any{
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_3",
			"name":         "read_file",
			"content": []any{
				map[string]any{"type": "input_text", "text": "line-1"},
				map[string]any{"type": "output_text", "text": "line-2"},
				map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"},
			},
		},
	}

	normalized := normalizeOpenAIMessagesForPrompt(raw)
	got, _ := normalized[0]["content"].(string)
	if !strings.Contains(got, "line-1\nline-2") {
		t.Fatalf("expected joined text blocks, got %q", got)
	}
}

func TestNormalizeOpenAIMessagesForPrompt_FunctionRoleCompatible(t *testing.T) {
	raw := []any{
		map[string]any{
			"role":         "function",
			"tool_call_id": "call_4",
			"name":         "legacy_tool",
			"content": map[string]any{
				"ok": true,
			},
		},
	}

	normalized := normalizeOpenAIMessagesForPrompt(raw)
	if len(normalized) != 1 {
		t.Fatalf("expected one normalized message, got %d", len(normalized))
	}
	if normalized[0]["role"] != "user" {
		t.Fatalf("expected function role mapped to user, got %#v", normalized[0]["role"])
	}
	got, _ := normalized[0]["content"].(string)
	if !strings.Contains(got, "name: legacy_tool") || !strings.Contains(got, `"ok":true`) {
		t.Fatalf("unexpected normalized function-role content: %q", got)
	}
}
