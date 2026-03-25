package util

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func BuildOpenAIChatCompletion(completionID, model, finalPrompt, finalThinking, finalText string, toolNames []string) map[string]any {
	detected := ParseToolCalls(finalText, toolNames)
	finishReason := "stop"
	messageObj := map[string]any{"role": "assistant", "content": finalText}
	if strings.TrimSpace(finalThinking) != "" {
		messageObj["reasoning_content"] = finalThinking
	}
	if len(detected) > 0 {
		finishReason = "tool_calls"
		messageObj["tool_calls"] = FormatOpenAIToolCalls(detected)
		messageObj["content"] = nil
	}
	promptTokens := EstimateTokens(finalPrompt)
	reasoningTokens := EstimateTokens(finalThinking)
	completionTokens := EstimateTokens(finalText)

	return map[string]any{
		"id":      completionID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": messageObj, "finish_reason": finishReason}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": reasoningTokens + completionTokens,
			"total_tokens":      promptTokens + reasoningTokens + completionTokens,
			"completion_tokens_details": map[string]any{
				"reasoning_tokens": reasoningTokens,
			},
		},
	}
}

func BuildOpenAIResponseObject(responseID, model, finalPrompt, finalThinking, finalText string, toolNames []string) map[string]any {
	detected := ParseToolCalls(finalText, toolNames)
	exposedOutputText := finalText
	output := make([]any, 0, 2)
	if len(detected) > 0 {
		// Keep structured tool output only; avoid leaking raw tool-call JSON
		// into response.output_text for clients reading completed responses.
		exposedOutputText = ""
		toolCalls := make([]any, 0, len(detected))
		for _, tc := range detected {
			toolCalls = append(toolCalls, map[string]any{
				"type":      "tool_call",
				"name":      tc.Name,
				"arguments": tc.Input,
			})
		}
		output = append(output, map[string]any{
			"type":       "tool_calls",
			"tool_calls": toolCalls,
		})
	} else {
		content := []any{
			map[string]any{
				"type": "output_text",
				"text": finalText,
			},
		}
		if finalThinking != "" {
			content = append([]any{map[string]any{
				"type": "reasoning",
				"text": finalThinking,
			}}, content...)
		}
		output = append(output, map[string]any{
			"type":    "message",
			"id":      "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"role":    "assistant",
			"content": content,
		})
	}
	promptTokens := EstimateTokens(finalPrompt)
	reasoningTokens := EstimateTokens(finalThinking)
	completionTokens := EstimateTokens(finalText)
	return map[string]any{
		"id":          responseID,
		"type":        "response",
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       model,
		"output":      output,
		"output_text": exposedOutputText,
		"usage": map[string]any{
			"input_tokens":  promptTokens,
			"output_tokens": reasoningTokens + completionTokens,
			"total_tokens":  promptTokens + reasoningTokens + completionTokens,
		},
	}
}

func BuildClaudeMessageResponse(messageID, model string, normalizedMessages []any, finalThinking, finalText string, toolNames []string) map[string]any {
	detected := ParseToolCalls(finalText, toolNames)
	content := make([]map[string]any, 0, 4)
	if finalThinking != "" {
		content = append(content, map[string]any{"type": "thinking", "thinking": finalThinking})
	}
	stopReason := "end_turn"
	if len(detected) > 0 {
		stopReason = "tool_use"
		for i, tc := range detected {
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    fmt.Sprintf("toolu_%d_%d", time.Now().Unix(), i),
				"name":  tc.Name,
				"input": tc.Input,
			})
		}
	} else {
		if finalText == "" {
			finalText = "抱歉，没有生成有效的响应内容。"
		}
		content = append(content, map[string]any{"type": "text", "text": finalText})
	}
	return map[string]any{
		"id":            messageID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  EstimateTokens(fmt.Sprintf("%v", normalizedMessages)),
			"output_tokens": EstimateTokens(finalThinking) + EstimateTokens(finalText),
		},
	}
}
