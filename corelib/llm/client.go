package llm

// Unified OpenAI-compatible LLM HTTP client.
// All packages (gui, tui, hub/corelib/agent) should use these functions
// instead of implementing their own request/response logic.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// DoOpenAIRequest sends a non-streaming OpenAI-compatible chat completion
// request. It handles provider quirks (e.g. MiniMax system-role merge)
// in one place so callers don't need to worry about them.
//
// The caller provides a context for cancellation/timeout control.
// tools may be nil for simple requests without tool calling.
func DoOpenAIRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"
	log.Printf("[LLM] POST %s model=%s protocol=%s", endpoint, cfg.Model, cfg.Protocol)

	// Provider-specific message adaptation
	if corelib.NeedsSystemMerge(cfg) {
		messages = corelib.MergeSystemIntoUser(messages)
	}

	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   false,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	// Detect SSE format: some gateways return streaming SSE even for non-stream requests.
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("data:")) || strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return ParseSSEToResponse(body)
	}

	var result Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	// Strip <think>, function_call, and XML tool_call blocks from content,
	// consistent with the streaming path.
	for i := range result.Choices {
		result.Choices[i].Message.Content = StripAllExtra(result.Choices[i].Message.Content)
	}
	return &result, nil
}

// sseChunk represents a single SSE chunk from an OpenAI-compatible streaming response.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string             `json:"content"`
			ReasoningContent string             `json:"reasoning_content"`
			ToolCalls        []sseToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

// sseToolCallDelta represents a tool call fragment in an SSE delta.
type sseToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// ParseSSEToResponse parses an SSE-formatted response body (lines prefixed
// with "data: ") into a single *Response by accumulating all chunks.
// This handles the case where an API gateway returns streaming SSE format
// even though the request did not ask for streaming.
func ParseSSEToResponse(body []byte) (*Response, error) {
	var contentBuf, reasoningBuf strings.Builder
	var finishReason string
	var usage *Usage

	// toolCalls accumulated by index
	type toolCallAcc struct {
		ID       string
		Type     string
		Name     string
		ArgsBuf  strings.Builder
	}
	toolCalls := make(map[int]*toolCallAcc)

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		contentBuf.WriteString(delta.Content)
		reasoningBuf.WriteString(delta.ReasoningContent)

		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
		}

		// Accumulate tool calls by index
		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &toolCallAcc{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			acc.ArgsBuf.WriteString(tc.Function.Arguments)
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}

	msg := Message{
		Role:             "assistant",
		Content:          StripAllExtra(contentBuf.String()),
		ReasoningContent: reasoningBuf.String(),
	}

	// Assemble tool calls in index order
	if len(toolCalls) > 0 {
		// Find max index
		maxIdx := 0
		for idx := range toolCalls {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc, ok := toolCalls[i]
			if !ok {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   acc.ID,
				Type: acc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      acc.Name,
					Arguments: acc.ArgsBuf.String(),
				},
			})
		}
	}

	return &Response{
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}, nil
}
