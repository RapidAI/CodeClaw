package openai

import (
	"encoding/json"
	"fmt"
	"strings"
)

func normalizeOpenAIMessagesForPrompt(raw []any) []map[string]any {
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		switch role {
		case "assistant":
			content := normalizeOpenAIContentForPrompt(msg["content"])
			toolCalls := formatAssistantToolCallsForPrompt(msg)
			combined := joinNonEmpty(content, toolCalls)
			if combined == "" {
				continue
			}
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": combined,
			})
		case "tool", "function":
			out = append(out, map[string]any{
				"role":    "user",
				"content": formatToolResultForPrompt(msg),
			})
		case "user", "system":
			out = append(out, map[string]any{
				"role":    role,
				"content": normalizeOpenAIContentForPrompt(msg["content"]),
			})
		default:
			content := normalizeOpenAIContentForPrompt(msg["content"])
			if content == "" {
				continue
			}
			if role == "" {
				role = "user"
			}
			out = append(out, map[string]any{
				"role":    role,
				"content": content,
			})
		}
	}
	return out
}

func formatAssistantToolCallsForPrompt(msg map[string]any) string {
	entries := make([]string, 0)
	if calls, ok := msg["tool_calls"].([]any); ok {
		for i, item := range calls {
			call, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(asString(call["id"]))
			if id == "" {
				id = fmt.Sprintf("call_%d", i+1)
			}
			name := strings.TrimSpace(asString(call["name"]))
			args := ""

			if fn, ok := call["function"].(map[string]any); ok {
				if name == "" {
					name = strings.TrimSpace(asString(fn["name"]))
				}
				args = normalizeOpenAIArgumentsForPrompt(fn["arguments"])
			}
			if name == "" {
				name = "unknown"
			}
			if args == "" {
				args = normalizeOpenAIArgumentsForPrompt(call["arguments"])
			}
			if args == "" {
				args = normalizeOpenAIArgumentsForPrompt(call["input"])
			}
			if args == "" {
				args = "{}"
			}
			entries = append(entries, fmt.Sprintf("Tool call:\n- tool_call_id: %s\n- function.name: %s\n- function.arguments: %s", id, name, args))
		}
	}

	if legacy, ok := msg["function_call"].(map[string]any); ok {
		name := strings.TrimSpace(asString(legacy["name"]))
		if name == "" {
			name = "unknown"
		}
		args := normalizeOpenAIArgumentsForPrompt(legacy["arguments"])
		if args == "" {
			args = "{}"
		}
		entries = append(entries, fmt.Sprintf("Tool call:\n- tool_call_id: call_legacy\n- function.name: %s\n- function.arguments: %s", name, args))
	}

	return strings.Join(entries, "\n\n")
}

func formatToolResultForPrompt(msg map[string]any) string {
	toolCallID := strings.TrimSpace(asString(msg["tool_call_id"]))
	if toolCallID == "" {
		toolCallID = strings.TrimSpace(asString(msg["id"]))
	}
	if toolCallID == "" {
		toolCallID = "unknown"
	}

	name := strings.TrimSpace(asString(msg["name"]))
	if name == "" {
		name = "unknown"
	}

	content := normalizeOpenAIContentForPrompt(msg["content"])
	if content == "" {
		content = "null"
	}

	return fmt.Sprintf("Tool result:\n- tool_call_id: %s\n- name: %s\n- content: %s", toolCallID, name, content)
}

func normalizeOpenAIContentForPrompt(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t := strings.ToLower(strings.TrimSpace(asString(m["type"])))
			if t != "text" && t != "output_text" && t != "input_text" {
				continue
			}
			if text := asString(m["text"]); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := asString(m["content"]); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return marshalToPromptString(v)
	}
}

func normalizeOpenAIArgumentsForPrompt(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return marshalToPromptString(v)
	}
}

func marshalToPromptString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return string(b)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func joinNonEmpty(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, p)
	}
	return strings.Join(nonEmpty, "\n\n")
}
