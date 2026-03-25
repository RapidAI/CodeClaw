package util

import "testing"

func TestBuildOpenAIChatCompletionWithToolCalls(t *testing.T) {
	out := BuildOpenAIChatCompletion(
		"cid1",
		"deepseek-chat",
		"prompt",
		"",
		`{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`,
		[]string{"search"},
	)
	if out["object"] != "chat.completion" {
		t.Fatalf("unexpected object: %#v", out["object"])
	}
	choices, _ := out["choices"].([]map[string]any)
	if len(choices) == 0 {
		// json-like map from generic marshalling may be []any in some paths
		rawChoices, _ := out["choices"].([]any)
		if len(rawChoices) == 0 {
			t.Fatalf("expected choices")
		}
		c0, _ := rawChoices[0].(map[string]any)
		if c0["finish_reason"] != "tool_calls" {
			t.Fatalf("expected finish_reason=tool_calls, got %#v", c0["finish_reason"])
		}
		return
	}
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls, got %#v", choices[0]["finish_reason"])
	}
}

func TestBuildOpenAIResponseObjectWithText(t *testing.T) {
	out := BuildOpenAIResponseObject(
		"resp_1",
		"gpt-4o",
		"prompt",
		"reasoning",
		"text",
		nil,
	)
	if out["object"] != "response" {
		t.Fatalf("unexpected object: %#v", out["object"])
	}
	output, _ := out["output"].([]any)
	if len(output) == 0 {
		t.Fatalf("expected output entries")
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "message" {
		t.Fatalf("expected first output type message, got %#v", first["type"])
	}
}

func TestBuildOpenAIResponseObjectToolCallsHidesRawOutputText(t *testing.T) {
	out := BuildOpenAIResponseObject(
		"resp_2",
		"gpt-4o",
		"prompt",
		"",
		`{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`,
		[]string{"search"},
	)
	if out["output_text"] != "" {
		t.Fatalf("expected empty output_text for tool_calls, got %#v", out["output_text"])
	}
	output, _ := out["output"].([]any)
	if len(output) == 0 {
		t.Fatalf("expected output entries")
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "tool_calls" {
		t.Fatalf("expected first output type tool_calls, got %#v", first["type"])
	}
}

func TestBuildClaudeMessageResponseToolUse(t *testing.T) {
	out := BuildClaudeMessageResponse(
		"msg_1",
		"claude-sonnet-4-5",
		[]any{map[string]any{"role": "user", "content": "hi"}},
		"",
		`{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`,
		[]string{"search"},
	)
	if out["type"] != "message" {
		t.Fatalf("unexpected type: %#v", out["type"])
	}
	if out["stop_reason"] != "tool_use" {
		t.Fatalf("expected stop_reason=tool_use, got %#v", out["stop_reason"])
	}
}
