package sse

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// ─── CollectStream edge cases ────────────────────────────────────────

func makeHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestCollectStreamEmpty(t *testing.T) {
	resp := makeHTTPResponse("")
	result := CollectStream(resp, false, false)
	if result.Text != "" || result.Thinking != "" {
		t.Fatalf("expected empty result, got text=%q think=%q", result.Text, result.Thinking)
	}
}

func TestCollectStreamTextOnly(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/content\",\"v\":\"Hello\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\" World\"}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, false, false)
	if result.Text != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", result.Text)
	}
	if result.Thinking != "" {
		t.Fatalf("expected no thinking, got %q", result.Thinking)
	}
}

func TestCollectStreamThinkingAndText(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/thinking_content\",\"v\":\"Thinking...\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\"Answer\"}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, true, true)
	if result.Thinking != "Thinking..." {
		t.Fatalf("expected 'Thinking...', got %q", result.Thinking)
	}
	if result.Text != "Answer" {
		t.Fatalf("expected 'Answer', got %q", result.Text)
	}
}

func TestCollectStreamOnlyThinking(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/thinking_content\",\"v\":\"Only thinking\"}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, true, true)
	if result.Thinking != "Only thinking" {
		t.Fatalf("expected 'Only thinking', got %q", result.Thinking)
	}
	if result.Text != "" {
		t.Fatalf("expected empty text, got %q", result.Text)
	}
}

func TestCollectStreamSkipsInvalidLines(t *testing.T) {
	resp := makeHTTPResponse(
		"event: comment\n" +
			"data: invalid_json\n" +
			"data: {\"p\":\"response/content\",\"v\":\"valid\"}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, false, false)
	if result.Text != "valid" {
		t.Fatalf("expected 'valid', got %q", result.Text)
	}
}

func TestCollectStreamWithFragments(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"type\":\"THINK\",\"content\":\"Think\"}]}\n" +
			"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"type\":\"RESPONSE\",\"content\":\"Done\"}]}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, true, true)
	if result.Thinking != "Think" {
		t.Fatalf("expected 'Think' thinking, got %q", result.Thinking)
	}
	if result.Text != "Done" {
		t.Fatalf("expected 'Done' text, got %q", result.Text)
	}
}

func TestCollectStreamWithCitation(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/content\",\"v\":\"Hello\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\"[citation:1] cited text\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\" more\"}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, false, false)
	// CollectStream does NOT filter citations (that's done by the adapters)
	// So citations are passed through as-is
	if !strings.Contains(result.Text, "[citation:1]") {
		t.Fatalf("expected citations to be passed through, got %q", result.Text)
	}
	if result.Text != "Hello[citation:1] cited text more" {
		t.Fatalf("expected full text with citation, got %q", result.Text)
	}
}

func TestCollectStreamMultipleThinkingChunks(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/thinking_content\",\"v\":\"part1\"}\n" +
			"data: {\"p\":\"response/thinking_content\",\"v\":\" part2\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\"answer\"}\n" +
			"data: [DONE]\n",
	)
	result := CollectStream(resp, true, true)
	if result.Thinking != "part1 part2" {
		t.Fatalf("expected 'part1 part2', got %q", result.Thinking)
	}
}

func TestCollectStreamStatusFinished(t *testing.T) {
	resp := makeHTTPResponse(
		"data: {\"p\":\"response/content\",\"v\":\"Hello\"}\n" +
			"data: {\"p\":\"response/status\",\"v\":\"FINISHED\"}\n",
	)
	result := CollectStream(resp, false, false)
	if result.Text != "Hello" {
		t.Fatalf("expected 'Hello', got %q", result.Text)
	}
}
