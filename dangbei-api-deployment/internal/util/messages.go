package util

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"ds2api/internal/config"
)

var markdownImagePattern = regexp.MustCompile(`!\[(.*?)\]\((.*?)\)`)

const ClaudeDefaultModel = "claude-sonnet-4-5"

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func MessagesPrepare(messages []map[string]any) string {
	type block struct {
		Role string
		Text string
	}
	processed := make([]block, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		text := normalizeContent(m["content"])
		processed = append(processed, block{Role: role, Text: text})
	}
	if len(processed) == 0 {
		return ""
	}
	merged := make([]block, 0, len(processed))
	for _, msg := range processed {
		if len(merged) > 0 && merged[len(merged)-1].Role == msg.Role {
			merged[len(merged)-1].Text += "\n\n" + msg.Text
			continue
		}
		merged = append(merged, msg)
	}
	parts := make([]string, 0, len(merged))
	for i, m := range merged {
		switch m.Role {
		case "assistant":
			parts = append(parts, "<｜Assistant｜>"+m.Text+"<｜end▁of▁sentence｜>")
		case "user", "system":
			if i > 0 {
				parts = append(parts, "<｜User｜>"+m.Text)
			} else {
				parts = append(parts, m.Text)
			}
		default:
			parts = append(parts, m.Text)
		}
	}
	out := strings.Join(parts, "")
	return markdownImagePattern.ReplaceAllString(out, `[${1}](${2})`)
}

func normalizeContent(v any) string {
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
			typeStr, _ := m["type"].(string)
			typeStr = strings.ToLower(strings.TrimSpace(typeStr))
			if typeStr == "text" || typeStr == "output_text" || typeStr == "input_text" {
				if txt, ok := m["text"].(string); ok {
					parts = append(parts, txt)
					continue
				}
				if txt, ok := m["content"].(string); ok {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

func ConvertClaudeToDeepSeek(claudeReq map[string]any, store *config.Store) map[string]any {
	messages, _ := claudeReq["messages"].([]any)
	model, _ := claudeReq["model"].(string)
	if model == "" {
		model = ClaudeDefaultModel
	}
	mapping := store.ClaudeMapping()
	dsModel := mapping["fast"]
	if dsModel == "" {
		dsModel = "deepseek-chat"
	}
	modelLower := strings.ToLower(model)
	if strings.Contains(modelLower, "opus") || strings.Contains(modelLower, "reasoner") || strings.Contains(modelLower, "slow") {
		if slow := mapping["slow"]; slow != "" {
			dsModel = slow
		}
	}
	convertedMessages := make([]any, 0, len(messages)+1)
	if system, ok := claudeReq["system"].(string); ok && system != "" {
		convertedMessages = append(convertedMessages, map[string]any{"role": "system", "content": system})
	}
	convertedMessages = append(convertedMessages, messages...)

	out := map[string]any{"model": dsModel, "messages": convertedMessages}
	for _, k := range []string{"temperature", "top_p", "stream"} {
		if v, ok := claudeReq[k]; ok {
			out[k] = v
		}
	}
	if stopSeq, ok := claudeReq["stop_sequences"]; ok {
		out["stop"] = stopSeq
	}
	return out
}

// EstimateTokens provides a rough token count approximation.
// For ASCII text (English, code, etc.) we use ~4 chars per token.
// For non-ASCII text (Chinese, Japanese, Korean, etc.) we use ~1.3 chars per token,
// which better reflects typical BPE tokenizer behavior for CJK scripts.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	asciiChars := 0
	nonASCIIChars := 0
	for _, r := range text {
		if r < 128 {
			asciiChars++
		} else {
			nonASCIIChars++
		}
	}
	// ASCII: ~4 chars per token; non-ASCII (CJK): ~1.3 chars per token
	n := asciiChars/4 + (nonASCIIChars*10+7)/13
	if n < 1 {
		return 1
	}
	return n
}
