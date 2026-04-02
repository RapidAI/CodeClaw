package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestBugCondition_SSE_SingleChunk verifies that DoOpenAIRequest can handle
// a single-chunk SSE response. On UNFIXED code this test FAILS because
// json.Unmarshal chokes on the "data: " prefix — confirming the bug exists.
//
// **Validates: Requirements 1.1, 1.3**
func TestBugCondition_SSE_SingleChunk(t *testing.T) {
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\ndata: [DONE]\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice in response")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
}

// TestBugCondition_SSE_MultiChunk verifies that DoOpenAIRequest can handle
// a multi-chunk SSE response with incremental content deltas. On UNFIXED code
// this test FAILS — confirming the bug exists.
//
// **Validates: Requirements 1.1, 1.3**
func TestBugCondition_SSE_MultiChunk(t *testing.T) {
	sseBody := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\"!\"}}]}",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice in response")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello world!" {
		t.Errorf("content = %q, want %q", got, "Hello world!")
	}
}

// TestBugCondition_RequestBody_MissingStreamFalse verifies that DoOpenAIRequest
// sends "stream": false in the request body. On UNFIXED code this test FAILS
// because the field is absent — confirming the bug exists.
//
// **Validates: Requirements 1.2**
func TestBugCondition_RequestBody_MissingStreamFalse(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		// Return valid JSON so the function doesn't error on the response side.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}

	var reqMap map[string]interface{}
	if err := json.Unmarshal(capturedBody, &reqMap); err != nil {
		t.Fatalf("failed to parse captured request body: %v", err)
	}

	streamVal, ok := reqMap["stream"]
	if !ok {
		t.Fatal("request body does not contain \"stream\" key — expected \"stream\": false")
	}
	streamBool, isBool := streamVal.(bool)
	if !isBool || streamBool != false {
		t.Errorf("stream = %v, want false", streamVal)
	}
}

// ---------------------------------------------------------------------------
// Preservation tests — these MUST PASS on the current (unfixed) code.
// They establish baseline behavior that must be preserved after the fix.
// ---------------------------------------------------------------------------

// TestPreservation_StandardJSONResponse verifies that DoOpenAIRequest correctly
// parses a standard JSON response with content and finish_reason.
//
// **Validates: Requirements 3.1**
func TestPreservation_StandardJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
	if got := resp.Choices[0].FinishReason; got != "stop" {
		t.Errorf("finish_reason = %q, want %q", got, "stop")
	}
}

// TestPreservation_HTTPErrorResponse500 verifies that DoOpenAIRequest returns
// an error containing "HTTP 500" when the server responds with status 500.
//
// **Validates: Requirements 3.2**
func TestPreservation_HTTPErrorResponse500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 500")
	}
}

// TestPreservation_ToolCallJSONResponse verifies that DoOpenAIRequest correctly
// parses a JSON response containing tool_calls.
//
// **Validates: Requirements 3.3**
func TestPreservation_ToolCallJSONResponse(t *testing.T) {
	toolCallResp := `{
		"choices":[{
			"message":{
				"role":"assistant",
				"content":"",
				"tool_calls":[{
					"id":"call_123",
					"type":"function",
					"function":{
						"name":"get_weather",
						"arguments":"{\"location\":\"Beijing\"}"
					}
				}]
			},
			"finish_reason":"tool_calls"
		}]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(toolCallResp))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "What is the weather?"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	choice := resp.Choices[0]
	if got := choice.FinishReason; got != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", got, "tool_calls")
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("tool call ID = %q, want %q", tc.ID, "call_123")
	}
	if tc.Type != "function" {
		t.Errorf("tool call type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool call function name = %q, want %q", tc.Function.Name, "get_weather")
	}
	if tc.Function.Arguments != `{"location":"Beijing"}` {
		t.Errorf("tool call arguments = %q, want %q", tc.Function.Arguments, `{"location":"Beijing"}`)
	}
}

// TestPreservation_HTTPErrorResponse400 verifies that DoOpenAIRequest returns
// an error containing "HTTP 400" when the server responds with status 400.
//
// **Validates: Requirements 3.2**
func TestPreservation_HTTPErrorResponse400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	_, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "HTTP 400")
	}
}

// ---------------------------------------------------------------------------
// Task 4 — Unit tests for ParseSSEToResponse edge cases
// ---------------------------------------------------------------------------

// TestParseSSE_SingleChunkTextContent verifies that ParseSSEToResponse with a
// single data line returns a Response with the correct content.
//
// **Validates: Requirements 2.2**
func TestParseSSE_SingleChunkTextContent(t *testing.T) {
	body := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\ndata: [DONE]\n")

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "Hello" {
		t.Errorf("content = %q, want %q", got, "Hello")
	}
	if got := resp.Choices[0].Message.Role; got != "assistant" {
		t.Errorf("role = %q, want %q", got, "assistant")
	}
}

// TestParseSSE_MultiChunkAccumulation verifies that ParseSSEToResponse with
// multiple data lines concatenates all content deltas.
//
// **Validates: Requirements 2.2**
func TestParseSSE_MultiChunkAccumulation(t *testing.T) {
	body := []byte(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"one\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" two\"}}]}",
		"data: {\"choices\":[{\"delta\":{\"content\":\" three\"}}]}",
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "one two three" {
		t.Errorf("content = %q, want %q", got, "one two three")
	}
}

// TestParseSSE_ToolCallDeltas verifies that ParseSSEToResponse correctly
// assembles tool calls from incremental SSE deltas with index, id, name,
// and argument fragments.
//
// **Validates: Requirements 2.2**
func TestParseSSE_ToolCallDeltas(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"NYC\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool call ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Type != "function" {
		t.Errorf("tool call type = %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool call name = %q, want %q", tc.Function.Name, "get_weather")
	}
	wantArgs := `{"location":"NYC"}`
	if tc.Function.Arguments != wantArgs {
		t.Errorf("tool call arguments = %q, want %q", tc.Function.Arguments, wantArgs)
	}
	if got := resp.Choices[0].FinishReason; got != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", got, "tool_calls")
	}
}

// TestParseSSE_ReasoningContent verifies that ParseSSEToResponse accumulates
// reasoning_content deltas from SSE chunks.
//
// **Validates: Requirements 2.2**
func TestParseSSE_ReasoningContent(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"Let me "}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"think..."}}]}`,
		`data: {"choices":[{"delta":{"content":"The answer is 42."}}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	msg := resp.Choices[0].Message
	if got := msg.ReasoningContent; got != "Let me think..." {
		t.Errorf("reasoning_content = %q, want %q", got, "Let me think...")
	}
	if got := msg.Content; got != "The answer is 42." {
		t.Errorf("content = %q, want %q", got, "The answer is 42.")
	}
}

// TestParseSSE_EmptyMalformedLines verifies that ParseSSEToResponse gracefully
// skips blank lines, lines without "data:" prefix, and malformed JSON, while
// still parsing valid chunks.
//
// **Validates: Requirements 2.2**
func TestParseSSE_EmptyMalformedLines(t *testing.T) {
	body := []byte(strings.Join([]string{
		"",
		"event: message",
		"data: {\"choices\":[{\"delta\":{\"content\":\"A\"}}]}",
		"",
		": this is a comment",
		"data: NOT-VALID-JSON",
		"data: {\"choices\":[{\"delta\":{\"content\":\"B\"}}]}",
		"random garbage line",
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "AB" {
		t.Errorf("content = %q, want %q", got, "AB")
	}
}

// TestParseSSE_StripAllExtra verifies that ParseSSEToResponse applies
// StripAllExtra to the accumulated content. The current StripThinkTags regex
// matches <think>...</think> followed by a literal backslash (due to \\s* in
// the raw string pattern). We use that pattern to confirm StripAllExtra is
// invoked on the accumulated SSE content.
//
// **Validates: Requirements 2.2**
func TestParseSSE_StripAllExtra(t *testing.T) {
	// The regex in filters.go uses `\\s*` in a raw string, which matches a
	// literal backslash after </think>. We craft content that matches this
	// pattern to prove StripAllExtra is called on the accumulated result.
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"<think>internal reasoning</think>\\"}}]}`,
		`data: {"choices":[{"delta":{"content":"visible answer"}}]}`,
		"data: [DONE]",
		"",
	}, "\n"))

	resp, err := ParseSSEToResponse(body)
	if err != nil {
		t.Fatalf("ParseSSEToResponse returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	got := resp.Choices[0].Message.Content
	if strings.Contains(got, "<think>") {
		t.Errorf("content still contains <think> tags: %q", got)
	}
	if got != "visible answer" {
		t.Errorf("content = %q, want %q", got, "visible answer")
	}
}

// TestParseSSE_SSEDetectionViaBodyPrefix verifies that DoOpenAIRequest detects
// SSE format via body prefix (data:) even when Content-Type is application/json,
// and parses it correctly via the SSE path.
//
// **Validates: Requirements 2.2**
func TestParseSSE_SSEDetectionViaBodyPrefix(t *testing.T) {
	sseBody := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"detected\"}}]}",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Intentionally set JSON content type, but body is SSE
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "detected" {
		t.Errorf("content = %q, want %q", got, "detected")
	}
}

// TestParseSSE_JSONPathUnchanged verifies that DoOpenAIRequest takes the JSON
// path (not SSE) when the response body starts with '{'.
//
// **Validates: Requirements 2.2**
func TestParseSSE_JSONPathUnchanged(t *testing.T) {
	jsonBody := `{"choices":[{"message":{"role":"assistant","content":"json-path"},"finish_reason":"stop"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(jsonBody))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:   srv.URL,
		Model: "test-model",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}

	resp, err := DoOpenAIRequest(context.Background(), cfg, messages, nil, srv.Client())
	if err != nil {
		t.Fatalf("DoOpenAIRequest returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if got := resp.Choices[0].Message.Content; got != "json-path" {
		t.Errorf("content = %q, want %q", got, "json-path")
	}
	if got := resp.Choices[0].FinishReason; got != "stop" {
		t.Errorf("finish_reason = %q, want %q", got, "stop")
	}
}
