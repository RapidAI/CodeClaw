package im

import "strings"

// ParseMentions extracts leading @name sequences from a message.
// It scans from the beginning, collecting consecutive @name tokens
// separated by spaces, stopping at the first non-@ token.
//
// Examples:
//
//	"@安妮 @小明 你们看看" → (["安妮", "小明"], "你们看看")
//	"@安妮 你好"           → (["安妮"], "你好")
//	"你好 @安妮"           → ([], "你好 @安妮")
//	"@安妮"                → (["安妮"], "")
func ParseMentions(text string) (names []string, body string) {
	text = strings.TrimSpace(text)
	if text == "" || text[0] != '@' {
		return nil, text
	}

	parts := strings.Fields(text)
	for i, p := range parts {
		if strings.HasPrefix(p, "@") && len(p) > 1 {
			names = append(names, p[1:])
		} else {
			body = strings.Join(parts[i:], " ")
			return names, body
		}
	}
	// All tokens were @mentions, body is empty.
	return names, ""
}
