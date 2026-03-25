package sse

import (
	"net/http"
	"strings"

	"ds2api/internal/deepseek"
)

// CollectResult holds the aggregated text and thinking content from a
// DeepSeek SSE stream, consumed to completion (non-streaming use case).
type CollectResult struct {
	Text     string
	Thinking string
}

// CollectStream fully consumes a DeepSeek SSE response and separates
// thinking content from text content. This replaces the duplicated
// stream-collection logic in openai.handleNonStream, claude.collectDeepSeek,
// and admin.testAccount.
//
// The caller is responsible for closing resp.Body unless closeBody is true.
func CollectStream(resp *http.Response, thinkingEnabled bool, closeBody bool) CollectResult {
	if closeBody {
		defer resp.Body.Close()
	}
	text := strings.Builder{}
	thinking := strings.Builder{}
	currentType := "text"
	if thinkingEnabled {
		currentType = "thinking"
	}
	_ = deepseek.ScanSSELines(resp, func(line []byte) bool {
		result := ParseDeepSeekContentLine(line, thinkingEnabled, currentType)
		currentType = result.NextType
		if !result.Parsed {
			return true
		}
		if result.Stop {
			return false
		}
		for _, p := range result.Parts {
			if p.Type == "thinking" {
				thinking.WriteString(p.Text)
			} else {
				text.WriteString(p.Text)
			}
		}
		return true
	})
	return CollectResult{Text: text.String(), Thinking: thinking.String()}
}
