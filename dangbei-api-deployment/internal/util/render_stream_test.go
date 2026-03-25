package util

import "testing"

func TestBuildOpenAIChatStreamChunk(t *testing.T) {
	chunk := BuildOpenAIChatStreamChunk(
		"cid",
		123,
		"deepseek-chat",
		[]map[string]any{BuildOpenAIChatStreamDeltaChoice(0, map[string]any{"role": "assistant"})},
		nil,
	)
	if chunk["object"] != "chat.completion.chunk" {
		t.Fatalf("unexpected object: %#v", chunk["object"])
	}
	choices, _ := chunk["choices"].([]map[string]any)
	if len(choices) == 0 {
		rawChoices, _ := chunk["choices"].([]any)
		if len(rawChoices) == 0 {
			t.Fatalf("expected choices")
		}
	}
}

func TestBuildOpenAIChatUsage(t *testing.T) {
	usage := BuildOpenAIChatUsage("prompt", "think", "answer")
	if _, ok := usage["prompt_tokens"]; !ok {
		t.Fatalf("expected prompt_tokens")
	}
	if _, ok := usage["completion_tokens_details"]; !ok {
		t.Fatalf("expected completion_tokens_details")
	}
}

func TestBuildOpenAIResponsesEventPayloads(t *testing.T) {
	created := BuildOpenAIResponsesCreatedPayload("resp_1", "gpt-4o")
	if created["type"] != "response.created" {
		t.Fatalf("unexpected type: %#v", created["type"])
	}
	done := BuildOpenAIResponsesToolCallDonePayload("resp_1", []map[string]any{{"index": 0}})
	if done["type"] != "response.output_tool_call.done" {
		t.Fatalf("unexpected type: %#v", done["type"])
	}
	completed := BuildOpenAIResponsesCompletedPayload(map[string]any{"id": "resp_1"})
	if completed["type"] != "response.completed" {
		t.Fatalf("unexpected type: %#v", completed["type"])
	}
}
