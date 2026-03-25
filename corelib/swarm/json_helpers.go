package swarm

import "strings"

// ExtractJSON tries to find a JSON array in the text (handles markdown fences).
func ExtractJSON(data []byte) []byte {
	s := string(data)
	// Strip markdown code fences
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	// Find the first [ and last ]
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start >= 0 && end > start {
		return []byte(s[start : end+1])
	}
	return []byte(strings.TrimSpace(s))
}
