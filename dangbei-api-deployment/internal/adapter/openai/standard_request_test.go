package openai

import (
	"testing"

	"ds2api/internal/config"
)

func newEmptyStoreForNormalizeTest(t *testing.T) *config.Store {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	return config.LoadStore()
}

func TestNormalizeOpenAIChatRequest(t *testing.T) {
	store := newEmptyStoreForNormalizeTest(t)
	req := map[string]any{
		"model": "gpt-5-codex",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"temperature": 0.3,
		"stream":      true,
	}
	n, err := normalizeOpenAIChatRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if n.ResolvedModel != "deepseek-reasoner" {
		t.Fatalf("unexpected resolved model: %s", n.ResolvedModel)
	}
	if !n.Stream {
		t.Fatalf("expected stream=true")
	}
	if _, ok := n.PassThrough["temperature"]; !ok {
		t.Fatalf("expected temperature passthrough")
	}
	if n.FinalPrompt == "" {
		t.Fatalf("expected non-empty final prompt")
	}
}

func TestNormalizeOpenAIResponsesRequestInput(t *testing.T) {
	store := newEmptyStoreForNormalizeTest(t)
	req := map[string]any{
		"model":        "gpt-4o",
		"input":        "ping",
		"instructions": "system",
	}
	n, err := normalizeOpenAIResponsesRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if n.ResolvedModel != "deepseek-chat" {
		t.Fatalf("unexpected resolved model: %s", n.ResolvedModel)
	}
	if len(n.Messages) != 2 {
		t.Fatalf("expected 2 normalized messages, got %d", len(n.Messages))
	}
}
