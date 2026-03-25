package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleResponsesStreamToolCallsHideRawOutputTextInCompleted(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	rawToolJSON := `{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}`
	streamBody := sseLine(rawToolJSON) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"})

	completed, ok := extractSSEEventPayload(rec.Body.String(), "response.completed")
	if !ok {
		t.Fatalf("expected response.completed event, body=%s", rec.Body.String())
	}
	responseObj, _ := completed["response"].(map[string]any)
	outputText, _ := responseObj["output_text"].(string)
	if outputText != "" {
		t.Fatalf("expected empty output_text for tool_calls response, got output_text=%q", outputText)
	}
	output, _ := responseObj["output"].([]any)
	if len(output) == 0 {
		t.Fatalf("expected structured output entries, got %#v", responseObj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "tool_calls" {
		t.Fatalf("expected first output type tool_calls, got %#v", first["type"])
	}
	toolCalls, _ := first["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		t.Fatalf("expected at least one tool_call in output, got %#v", first["tool_calls"])
	}
	call0, _ := toolCalls[0].(map[string]any)
	if call0["name"] != "read_file" {
		t.Fatalf("unexpected tool call name: %#v", call0["name"])
	}
	if strings.Contains(outputText, `"tool_calls"`) {
		t.Fatalf("raw tool_calls JSON leaked in output_text: %q", outputText)
	}
}

func TestHandleResponsesStreamIncompleteTailNotDuplicatedInCompletedOutputText(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	tail := `{"tool_calls":[{"name":"read_file","input":`
	streamBody := sseLine("Before ") + sseLine(tail) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"})

	completed, ok := extractSSEEventPayload(rec.Body.String(), "response.completed")
	if !ok {
		t.Fatalf("expected response.completed event, body=%s", rec.Body.String())
	}
	responseObj, _ := completed["response"].(map[string]any)
	outputText, _ := responseObj["output_text"].(string)
	if strings.Count(outputText, tail) > 1 {
		t.Fatalf("expected incomplete tail not to be duplicated, got output_text=%q", outputText)
	}
}

func extractSSEEventPayload(body, targetEvent string) (map[string]any, bool) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	matched := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			matched = evt == targetEvent
			continue
		}
		if !matched || !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, false
		}
		return payload, true
	}
	return nil, false
}
