package sse

import (
	"bytes"
	"encoding/json"
	"strings"
)

type ContentPart struct {
	Text string
	Type string
}

var skipPatterns = []string{
	"quasi_status", "elapsed_secs", "token_usage", "pending_fragment", "conversation_mode",
	"fragments/-1/status", "fragments/-2/status", "fragments/-3/status",
}

func ParseDeepSeekSSELine(raw []byte) (map[string]any, bool, bool) {
	line := strings.TrimSpace(string(raw))
	if line == "" || !strings.HasPrefix(line, "data:") {
		return nil, false, false
	}
	dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if dataStr == "[DONE]" {
		return nil, true, true
	}
	chunk := map[string]any{}
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return nil, false, false
	}
	return chunk, false, true
}

func shouldSkipPath(path string) bool {
	if path == "response/search_status" {
		return true
	}
	for _, p := range skipPatterns {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}

func ParseSSEChunkForContent(chunk map[string]any, thinkingEnabled bool, currentFragmentType string) ([]ContentPart, bool, string) {
	v, ok := chunk["v"]
	if !ok {
		return nil, false, currentFragmentType
	}
	path, _ := chunk["p"].(string)
	if shouldSkipPath(path) {
		return nil, false, currentFragmentType
	}
	if path == "response/status" {
		if s, ok := v.(string); ok && s == "FINISHED" {
			return nil, true, currentFragmentType
		}
	}
	newType := currentFragmentType
	parts := make([]ContentPart, 0, 8)

	// Newer DeepSeek responses may emit fragment APPEND directly on
	// path "response/fragments" instead of wrapping it in path "response".
	if path == "response/fragments" {
		if op, _ := chunk["o"].(string); strings.EqualFold(op, "APPEND") {
			if frags, ok := v.([]any); ok {
				for _, frag := range frags {
					fm, ok := frag.(map[string]any)
					if !ok {
						continue
					}
					t, _ := fm["type"].(string)
					content, _ := fm["content"].(string)
					t = strings.ToUpper(t)
					switch t {
					case "THINK", "THINKING":
						newType = "thinking"
						if content != "" {
							parts = append(parts, ContentPart{Text: content, Type: "thinking"})
						}
					case "RESPONSE":
						newType = "text"
						if content != "" {
							parts = append(parts, ContentPart{Text: content, Type: "text"})
						}
					default:
						if content != "" {
							parts = append(parts, ContentPart{Text: content, Type: "text"})
						}
					}
				}
			}
		}
	}

	if path == "response" {
		if arr, ok := v.([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				if m["p"] == "fragments" && m["o"] == "APPEND" {
					if frags, ok := m["v"].([]any); ok {
						for _, frag := range frags {
							fm, ok := frag.(map[string]any)
							if !ok {
								continue
							}
							t, _ := fm["type"].(string)
							t = strings.ToUpper(t)
							if t == "THINK" || t == "THINKING" {
								newType = "thinking"
							} else if t == "RESPONSE" {
								newType = "text"
							}
						}
					}
				}
			}
		}
	}
	partType := "text"
	switch {
	case path == "response/thinking_content":
		partType = "thinking"
	case path == "response/content":
		partType = "text"
	case strings.Contains(path, "response/fragments") && strings.Contains(path, "/content"):
		partType = newType
	case path == "":
		if thinkingEnabled {
			partType = newType
		}
	}
	switch val := v.(type) {
	case string:
		if val == "FINISHED" && (path == "" || path == "status") {
			return nil, true, newType
		}
		if val != "" {
			parts = append(parts, ContentPart{Text: val, Type: partType})
		}
	case []any:
		pp, finished := extractContentRecursive(val, partType)
		if finished {
			return nil, true, newType
		}
		parts = append(parts, pp...)
	case map[string]any:
		resp := val
		if wrapped, ok := val["response"].(map[string]any); ok {
			resp = wrapped
		}
		if frags, ok := resp["fragments"].([]any); ok {
			for _, item := range frags {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				t, _ := m["type"].(string)
				content, _ := m["content"].(string)
				t = strings.ToUpper(t)
				if t == "THINK" || t == "THINKING" {
					newType = "thinking"
					if content != "" {
						parts = append(parts, ContentPart{Text: content, Type: "thinking"})
					}
				} else if t == "RESPONSE" {
					newType = "text"
					if content != "" {
						parts = append(parts, ContentPart{Text: content, Type: "text"})
					}
				} else if content != "" {
					parts = append(parts, ContentPart{Text: content, Type: partType})
				}
			}
		}
	}
	return parts, false, newType
}

func extractContentRecursive(items []any, defaultType string) ([]ContentPart, bool) {
	parts := make([]ContentPart, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		itemPath, _ := m["p"].(string)
		itemV, hasV := m["v"]
		if !hasV {
			continue
		}
		if itemPath == "status" {
			if s, ok := itemV.(string); ok && s == "FINISHED" {
				return nil, true
			}
		}
		if shouldSkipPath(itemPath) {
			continue
		}
		if content, ok := m["content"].(string); ok && content != "" {
			typeName, _ := m["type"].(string)
			typeName = strings.ToUpper(typeName)
			switch typeName {
			case "THINK", "THINKING":
				parts = append(parts, ContentPart{Text: content, Type: "thinking"})
			case "RESPONSE":
				parts = append(parts, ContentPart{Text: content, Type: "text"})
			default:
				parts = append(parts, ContentPart{Text: content, Type: defaultType})
			}
			continue
		}
		partType := defaultType
		if strings.Contains(itemPath, "thinking") {
			partType = "thinking"
		} else if strings.Contains(itemPath, "content") || itemPath == "response" || itemPath == "fragments" {
			partType = "text"
		}
		switch v := itemV.(type) {
		case string:
			if v != "" && v != "FINISHED" {
				parts = append(parts, ContentPart{Text: v, Type: partType})
			}
		case []any:
			for _, inner := range v {
				switch x := inner.(type) {
				case map[string]any:
					ct, _ := x["content"].(string)
					if ct == "" {
						continue
					}
					typeName, _ := x["type"].(string)
					typeName = strings.ToUpper(typeName)
					if typeName == "THINK" || typeName == "THINKING" {
						parts = append(parts, ContentPart{Text: ct, Type: "thinking"})
					} else if typeName == "RESPONSE" {
						parts = append(parts, ContentPart{Text: ct, Type: "text"})
					} else {
						parts = append(parts, ContentPart{Text: ct, Type: partType})
					}
				case string:
					if x != "" {
						parts = append(parts, ContentPart{Text: x, Type: partType})
					}
				}
			}
		}
	}
	return parts, false
}

func IsCitation(text string) bool {
	return bytes.HasPrefix([]byte(strings.TrimSpace(text)), []byte("[citation:"))
}
