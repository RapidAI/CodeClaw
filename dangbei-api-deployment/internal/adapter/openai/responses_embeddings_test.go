package openai

import (
	"testing"
	"time"
)

func TestNormalizeResponsesInputAsMessagesString(t *testing.T) {
	msgs := normalizeResponsesInputAsMessages("hello")
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	m, _ := msgs[0].(map[string]any)
	if m["role"] != "user" || m["content"] != "hello" {
		t.Fatalf("unexpected message: %#v", m)
	}
}

func TestResponsesMessagesFromRequestWithInstructions(t *testing.T) {
	req := map[string]any{
		"model":        "gpt-4.1",
		"input":        "ping",
		"instructions": "system text",
	}
	msgs := responsesMessagesFromRequest(req)
	if len(msgs) != 2 {
		t.Fatalf("expected two messages, got %d", len(msgs))
	}
	sys, _ := msgs[0].(map[string]any)
	if sys["role"] != "system" {
		t.Fatalf("unexpected first message: %#v", sys)
	}
}

func TestExtractEmbeddingInputs(t *testing.T) {
	got := extractEmbeddingInputs([]any{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected inputs: %#v", got)
	}
}

func TestDeterministicEmbeddingStable(t *testing.T) {
	a := deterministicEmbedding("hello")
	b := deterministicEmbedding("hello")
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("expected 64 dims, got %d and %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("expected stable embedding at %d: %v != %v", i, a[i], b[i])
		}
	}
}

func TestResponseStorePutGet(t *testing.T) {
	st := newResponseStore(100 * time.Millisecond)
	st.put("owner_1", "resp_1", map[string]any{"id": "resp_1"})
	got, ok := st.get("owner_1", "resp_1")
	if !ok {
		t.Fatal("expected stored response")
	}
	if got["id"] != "resp_1" {
		t.Fatalf("unexpected response payload: %#v", got)
	}
}

func TestResponseStoreTenantIsolation(t *testing.T) {
	st := newResponseStore(100 * time.Millisecond)
	st.put("owner_a", "resp_1", map[string]any{"id": "resp_1"})
	if _, ok := st.get("owner_b", "resp_1"); ok {
		t.Fatal("expected owner_b to be isolated from owner_a response")
	}
}
