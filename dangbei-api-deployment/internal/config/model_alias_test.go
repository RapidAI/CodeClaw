package config

import "testing"

func TestResolveModelDirectDeepSeek(t *testing.T) {
	got, ok := ResolveModel(nil, "deepseek-chat")
	if !ok || got != "deepseek-chat" {
		t.Fatalf("expected deepseek-chat, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelAlias(t *testing.T) {
	got, ok := ResolveModel(nil, "gpt-4.1")
	if !ok || got != "deepseek-chat" {
		t.Fatalf("expected alias gpt-4.1 -> deepseek-chat, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelHeuristicReasoner(t *testing.T) {
	got, ok := ResolveModel(nil, "o3-super")
	if !ok || got != "deepseek-reasoner" {
		t.Fatalf("expected heuristic reasoner, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelUnknown(t *testing.T) {
	_, ok := ResolveModel(nil, "totally-custom-model")
	if ok {
		t.Fatal("expected unknown model to fail resolve")
	}
}

func TestClaudeModelsResponsePaginationFields(t *testing.T) {
	resp := ClaudeModelsResponse()
	if _, ok := resp["first_id"]; !ok {
		t.Fatalf("expected first_id in response: %#v", resp)
	}
	if _, ok := resp["last_id"]; !ok {
		t.Fatalf("expected last_id in response: %#v", resp)
	}
	if _, ok := resp["has_more"]; !ok {
		t.Fatalf("expected has_more in response: %#v", resp)
	}
}
