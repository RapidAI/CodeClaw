package util

func BuildOpenAIChatStreamDeltaChoice(index int, delta map[string]any) map[string]any {
	return map[string]any{
		"delta": delta,
		"index": index,
	}
}

func BuildOpenAIChatStreamFinishChoice(index int, finishReason string) map[string]any {
	return map[string]any{
		"delta":         map[string]any{},
		"index":         index,
		"finish_reason": finishReason,
	}
}

func BuildOpenAIChatStreamChunk(completionID string, created int64, model string, choices []map[string]any, usage map[string]any) map[string]any {
	out := map[string]any{
		"id":      completionID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": choices,
	}
	if len(usage) > 0 {
		out["usage"] = usage
	}
	return out
}

func BuildOpenAIChatUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	promptTokens := EstimateTokens(finalPrompt)
	reasoningTokens := EstimateTokens(finalThinking)
	completionTokens := EstimateTokens(finalText)
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": reasoningTokens + completionTokens,
		"total_tokens":      promptTokens + reasoningTokens + completionTokens,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": reasoningTokens,
		},
	}
}

func BuildOpenAIResponsesCreatedPayload(responseID, model string) map[string]any {
	return map[string]any{
		"type":   "response.created",
		"id":     responseID,
		"object": "response",
		"model":  model,
		"status": "in_progress",
	}
}

func BuildOpenAIResponsesTextDeltaPayload(responseID, delta string) map[string]any {
	return map[string]any{
		"type":  "response.output_text.delta",
		"id":    responseID,
		"delta": delta,
	}
}

func BuildOpenAIResponsesReasoningDeltaPayload(responseID, delta string) map[string]any {
	return map[string]any{
		"type":  "response.reasoning.delta",
		"id":    responseID,
		"delta": delta,
	}
}

func BuildOpenAIResponsesToolCallDeltaPayload(responseID string, toolCalls []map[string]any) map[string]any {
	return map[string]any{
		"type":       "response.output_tool_call.delta",
		"id":         responseID,
		"tool_calls": toolCalls,
	}
}

func BuildOpenAIResponsesToolCallDonePayload(responseID string, toolCalls []map[string]any) map[string]any {
	return map[string]any{
		"type":       "response.output_tool_call.done",
		"id":         responseID,
		"tool_calls": toolCalls,
	}
}

func BuildOpenAIResponsesCompletedPayload(response map[string]any) map[string]any {
	return map[string]any{
		"type":     "response.completed",
		"response": response,
	}
}
