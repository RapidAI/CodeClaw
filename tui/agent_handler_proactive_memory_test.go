package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ---------------------------------------------------------------------------
// Task 6.3: Unit tests for proactive memory instruction in TUI system prompt
// ---------------------------------------------------------------------------

func TestTUISystemPrompt_WithMemoryStore_ContainsProactiveInstruction(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer ms.Stop()

	h := &TUIAgentHandler{
		memoryStore:      ms,
		codingToolHealth: newCodingToolHealthCache(),
	}

	prompt := h.buildSystemPrompt()

	keywords := []string{
		"主动记忆",
		"proactive",
		"memory(action=save)",
		"每次会话最多主动保存 5 条",
		"💾 已主动记录",
	}
	for _, kw := range keywords {
		if !strings.Contains(prompt, kw) {
			t.Errorf("TUI prompt with memoryStore missing keyword %q", kw)
		}
	}
}

func TestTUISystemPrompt_WithoutMemoryStore_NoProactiveInstruction(t *testing.T) {
	h := &TUIAgentHandler{
		codingToolHealth: newCodingToolHealthCache(),
	}

	prompt := h.buildSystemPrompt()

	if strings.Contains(prompt, "主动记忆") {
		t.Error("TUI prompt without memoryStore should not contain proactive memory instruction")
	}
}
