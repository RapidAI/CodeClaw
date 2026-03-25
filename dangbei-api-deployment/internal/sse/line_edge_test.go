package sse

import "testing"

func TestParseDeepSeekContentLineNotParsed(t *testing.T) {
	res := ParseDeepSeekContentLine([]byte("not a data line"), false, "text")
	if res.Parsed {
		t.Fatal("expected not parsed")
	}
	if res.NextType != "text" {
		t.Fatalf("expected nextType preserved, got %q", res.NextType)
	}
}

func TestParseDeepSeekContentLinePreservesNextType(t *testing.T) {
	res := ParseDeepSeekContentLine([]byte(`data: {"p":"response/thinking_content","v":"think"}`), true, "thinking")
	if !res.Parsed || res.Stop {
		t.Fatalf("expected parsed non-stop: %#v", res)
	}
	if len(res.Parts) != 1 || res.Parts[0].Type != "thinking" {
		t.Fatalf("unexpected parts: %#v", res.Parts)
	}
}

func TestParseDeepSeekContentLineFragmentSwitchType(t *testing.T) {
	res := ParseDeepSeekContentLine(
		[]byte(`data: {"p":"response/fragments","o":"APPEND","v":[{"type":"RESPONSE","content":"hi"}]}`),
		true, "thinking",
	)
	if !res.Parsed || res.Stop {
		t.Fatalf("expected parsed non-stop: %#v", res)
	}
	if res.NextType != "text" {
		t.Fatalf("expected nextType text after RESPONSE fragment, got %q", res.NextType)
	}
}

func TestParseDeepSeekContentLineContentFilterMessage(t *testing.T) {
	res := ParseDeepSeekContentLine([]byte(`data: {"code":"content_filter"}`), false, "text")
	if !res.ContentFilter {
		t.Fatal("expected content filter flag")
	}
	if res.ErrorMessage == "" {
		t.Fatal("expected error message on content filter")
	}
}

func TestParseDeepSeekContentLineErrorObjectFormat(t *testing.T) {
	res := ParseDeepSeekContentLine([]byte(`data: {"error":{"message":"rate limit","code":429}}`), false, "text")
	if !res.Parsed || !res.Stop {
		t.Fatalf("expected parsed stop: %#v", res)
	}
	if res.ErrorMessage == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestParseDeepSeekContentLineInvalidJSON(t *testing.T) {
	res := ParseDeepSeekContentLine([]byte("data: {broken"), false, "text")
	if res.Parsed {
		t.Fatal("expected not parsed for broken JSON")
	}
}

func TestParseDeepSeekContentLineEmptyBytes(t *testing.T) {
	res := ParseDeepSeekContentLine([]byte{}, false, "text")
	if res.Parsed {
		t.Fatal("expected not parsed for empty bytes")
	}
}
