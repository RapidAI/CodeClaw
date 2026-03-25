package util

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var toolCallPattern = regexp.MustCompile(`\{\s*["']tool_calls["']\s*:\s*\[(.*?)\]\s*\}`)
var fencedJSONPattern = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
var fencedBlockPattern = regexp.MustCompile("(?s)```.*?```")

type ParsedToolCall struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func ParseToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	text = stripFencedCodeBlocks(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	candidates := buildToolCallCandidates(text)
	var parsed []ParsedToolCall
	for _, candidate := range candidates {
		if tc := parseToolCallsPayload(candidate); len(tc) > 0 {
			parsed = tc
			break
		}
	}
	if len(parsed) == 0 {
		return nil
	}

	return filterToolCalls(parsed, availableToolNames)
}

func ParseStandaloneToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if looksLikeToolExampleContext(trimmed) {
		return nil
	}
	candidates := []string{trimmed}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !strings.HasPrefix(candidate, "{") && !strings.HasPrefix(candidate, "[") {
			continue
		}
		if parsed := parseToolCallsPayload(candidate); len(parsed) > 0 {
			return filterToolCalls(parsed, availableToolNames)
		}
	}
	return nil
}

func filterToolCalls(parsed []ParsedToolCall, availableToolNames []string) []ParsedToolCall {
	allowed := map[string]struct{}{}
	for _, name := range availableToolNames {
		allowed[name] = struct{}{}
	}
	out := make([]ParsedToolCall, 0, len(parsed))
	for _, tc := range parsed {
		if tc.Name == "" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[tc.Name]; !ok {
				continue
			}
		}
		if tc.Input == nil {
			tc.Input = map[string]any{}
		}
		out = append(out, tc)
	}
	// If the model clearly emitted tool_calls JSON but all names are outside the
	// declared set, keep the parsed calls as a fallback so upper layers can still
	// intercept structured tool output instead of leaking raw JSON to users.
	if len(out) == 0 && len(parsed) > 0 {
		for _, tc := range parsed {
			if tc.Name == "" {
				continue
			}
			if tc.Input == nil {
				tc.Input = map[string]any{}
			}
			out = append(out, tc)
		}
	}
	return out
}

func buildToolCallCandidates(text string) []string {
	trimmed := strings.TrimSpace(text)
	candidates := []string{trimmed}

	// fenced code block candidates: ```json ... ```
	for _, match := range fencedJSONPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(match) >= 2 {
			candidates = append(candidates, strings.TrimSpace(match[1]))
		}
	}

	// best-effort extraction around "tool_calls" key in mixed text payloads.
	candidates = append(candidates, extractToolCallObjects(trimmed)...)

	// best-effort object slice: from first '{' to last '}'
	first := strings.Index(trimmed, "{")
	last := strings.LastIndex(trimmed, "}")
	if first >= 0 && last > first {
		candidates = append(candidates, strings.TrimSpace(trimmed[first:last+1]))
	}

	// legacy regex extraction fallback
	if m := toolCallPattern.FindStringSubmatch(trimmed); len(m) >= 2 {
		candidates = append(candidates, "{"+`"tool_calls":[`+m[1]+"]}")
	}

	uniq := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		uniq = append(uniq, c)
	}
	return uniq
}

func parseToolCallsPayload(payload string) []ParsedToolCall {
	var decoded any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil
	}
	switch v := decoded.(type) {
	case map[string]any:
		if tc, ok := v["tool_calls"]; ok {
			return parseToolCallList(tc)
		}
		if parsed, ok := parseToolCallItem(v); ok {
			return []ParsedToolCall{parsed}
		}
	case []any:
		return parseToolCallList(v)
	}
	return nil
}

func parseToolCallList(v any) []ParsedToolCall {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tc, ok := parseToolCallItem(m); ok {
			out = append(out, tc)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseToolCallItem(m map[string]any) (ParsedToolCall, bool) {
	name, _ := m["name"].(string)
	inputRaw, hasInput := m["input"]
	if fn, ok := m["function"].(map[string]any); ok {
		if name == "" {
			name, _ = fn["name"].(string)
		}
		if !hasInput {
			if v, ok := fn["arguments"]; ok {
				inputRaw = v
				hasInput = true
			}
		}
	}
	if !hasInput {
		for _, key := range []string{"arguments", "args", "parameters", "params"} {
			if v, ok := m[key]; ok {
				inputRaw = v
				hasInput = true
				break
			}
		}
	}
	if strings.TrimSpace(name) == "" {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{
		Name:  strings.TrimSpace(name),
		Input: parseToolCallInput(inputRaw),
	}, true
}

func parseToolCallInput(v any) map[string]any {
	switch x := v.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return x
	case string:
		raw := strings.TrimSpace(x)
		if raw == "" {
			return map[string]any{}
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed != nil {
			return parsed
		}
		return map[string]any{"_raw": raw}
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return map[string]any{}
		}
		var parsed map[string]any
		if err := json.Unmarshal(b, &parsed); err == nil && parsed != nil {
			return parsed
		}
		return map[string]any{}
	}
}

func extractToolCallObjects(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	out := []string{}
	offset := 0
	for {
		idx := strings.Index(lower[offset:], "tool_calls")
		if idx < 0 {
			break
		}
		idx += offset
		start := strings.LastIndex(text[:idx], "{")
		for start >= 0 {
			candidate, end, ok := extractJSONObject(text, start)
			if ok {
				// Move forward to avoid repeatedly matching the same object.
				offset = end
				out = append(out, strings.TrimSpace(candidate))
				break
			}
			start = strings.LastIndex(text[:start], "{")
		}
		if start < 0 {
			offset = idx + len("tool_calls")
		}
	}
	return out
}

func extractJSONObject(text string, start int) (string, int, bool) {
	if start < 0 || start >= len(text) || text[start] != '{' {
		return "", 0, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return text[start : i+1], i + 1, true
			}
		}
	}
	return "", 0, false
}

func looksLikeToolExampleContext(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	return strings.Contains(t, "```")
}

func stripFencedCodeBlocks(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return fencedBlockPattern.ReplaceAllString(text, " ")
}

func FormatOpenAIToolCalls(calls []ParsedToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		args, _ := json.Marshal(c.Input)
		out = append(out, map[string]any{
			"id":   "call_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type": "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": string(args),
			},
		})
	}
	return out
}

func FormatOpenAIStreamToolCalls(calls []ParsedToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for i, c := range calls {
		args, _ := json.Marshal(c.Input)
		out = append(out, map[string]any{
			"index": i,
			"id":    "call_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type":  "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": string(args),
			},
		})
	}
	return out
}
