package freeproxy

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ToolCall represents an OpenAI-compatible tool call extracted from model output.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction holds the function name and arguments of a tool call.
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

var toolCallBlockRe = regexp.MustCompile("(?s)```tool_call\\s*\\n?(.*?)\\n?```")

// ParseToolCalls extracts tool calls from model output that uses the
// ```tool_call ... ``` code block convention (same as dangbei-api-deployment).
func ParseToolCalls(content string) []ToolCall {
	matches := toolCallBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	var calls []ToolCall
	for i, m := range matches {
		if len(m) < 2 {
			continue
		}
		raw := strings.TrimSpace(m[1])
		var parsed struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		argsBytes, _ := json.Marshal(parsed.Arguments)
		calls = append(calls, ToolCall{
			ID:       generateToolCallID(i),
			Type:     "function",
			Function: ToolFunction{Name: parsed.Name, Arguments: string(argsBytes)},
		})
	}
	return calls
}

// RemoveToolCallBlocks strips ```tool_call ... ``` blocks from content.
func RemoveToolCallBlocks(content string) string {
	return strings.TrimSpace(toolCallBlockRe.ReplaceAllString(content, ""))
}

// HasToolCalls checks whether content contains tool call blocks.
func HasToolCalls(content string) bool {
	return strings.Contains(content, "```tool_call")
}

func generateToolCallID(_ int) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("call_%x", b)
}
