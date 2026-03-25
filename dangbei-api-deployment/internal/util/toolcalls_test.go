package util

import "testing"

func TestParseToolCalls(t *testing.T) {
	text := `prefix {"tool_calls":[{"name":"search","input":{"q":"golang"}}]} suffix`
	calls := ParseToolCalls(text, []string{"search"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "search" {
		t.Fatalf("unexpected tool name: %s", calls[0].Name)
	}
	if calls[0].Input["q"] != "golang" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseToolCallsFromFencedJSON(t *testing.T) {
	text := "I will call tools now\n```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"news\"}}]}\n```"
	calls := ParseToolCalls(text, []string{"search"})
	if len(calls) != 0 {
		t.Fatalf("expected fenced tool_call example to be ignored, got %#v", calls)
	}
}

func TestParseToolCallsWithFunctionArgumentsString(t *testing.T) {
	text := `{"tool_calls":[{"function":{"name":"get_weather","arguments":"{\"city\":\"beijing\"}"}}]}`
	calls := ParseToolCalls(text, []string{"get_weather"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "get_weather" {
		t.Fatalf("unexpected tool name: %s", calls[0].Name)
	}
	if calls[0].Input["city"] != "beijing" {
		t.Fatalf("unexpected args: %#v", calls[0].Input)
	}
}

func TestParseToolCallsKeepsUnknownAsFallback(t *testing.T) {
	text := `{"tool_calls":[{"name":"unknown","input":{}}]}`
	calls := ParseToolCalls(text, []string{"search"})
	if len(calls) != 1 {
		t.Fatalf("expected fallback 1 call, got %d", len(calls))
	}
	if calls[0].Name != "unknown" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
}

func TestFormatOpenAIToolCalls(t *testing.T) {
	formatted := FormatOpenAIToolCalls([]ParsedToolCall{{Name: "search", Input: map[string]any{"q": "x"}}})
	if len(formatted) != 1 {
		t.Fatalf("expected 1, got %d", len(formatted))
	}
	fn, _ := formatted[0]["function"].(map[string]any)
	if fn["name"] != "search" {
		t.Fatalf("unexpected function name: %#v", fn)
	}
}

func TestParseStandaloneToolCallsOnlyMatchesStandalonePayload(t *testing.T) {
	mixed := `这里是示例：{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`
	if calls := ParseStandaloneToolCalls(mixed, []string{"search"}); len(calls) != 0 {
		t.Fatalf("expected standalone parser to ignore mixed prose, got %#v", calls)
	}

	standalone := `{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`
	calls := ParseStandaloneToolCalls(standalone, []string{"search"})
	if len(calls) != 1 {
		t.Fatalf("expected standalone parser to match, got %#v", calls)
	}
}

func TestParseStandaloneToolCallsIgnoresFencedCodeBlock(t *testing.T) {
	fenced := "```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"go\"}}]}\n```"
	if calls := ParseStandaloneToolCalls(fenced, []string{"search"}); len(calls) != 0 {
		t.Fatalf("expected fenced tool_call example to be ignored, got %#v", calls)
	}
}
