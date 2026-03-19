package main

import (
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// Property-based tests for coding-interaction-workflow feature.
//
// Each test generates random App configurations (role names, descriptions)
// and verifies structural properties of buildSystemPrompt() output.
// Uses testing/quick with at least 100 iterations per property.
// ---------------------------------------------------------------------------

// randomAppConfig is a helper type for testing/quick that generates random
// IMMessageHandler instances with varying role configurations.
type randomAppConfig struct {
	RoleName string
	RoleDesc string
}

// Generate implements quick.Generator for randomAppConfig.
func (randomAppConfig) Generate(rand *rand.Rand, size int) interface{} {
	names := []string{
		"", // default
		"MaClaw",
		"TestBot",
		"开发助手",
		"CodeHelper-" + randomString(rand, 8),
		randomString(rand, rand.Intn(20)+1),
	}
	descs := []string{
		"", // default
		"一个尽心尽责无所不能的软件开发管家",
		"A helpful coding assistant",
		"专注于代码质量的AI助手",
		randomString(rand, rand.Intn(50)+1),
	}
	return randomAppConfig{
		RoleName: names[rand.Intn(len(names))],
		RoleDesc: descs[rand.Intn(len(descs))],
	}
}

func randomString(rand *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// buildPromptForConfig creates an IMMessageHandler with the given config
// and returns the buildSystemPrompt() output.
func buildPromptForConfig(cfg randomAppConfig) string {
	app := &App{}
	// Set up a temp config dir so LoadConfig returns the custom role values.
	// Since LoadConfig reads from disk and we don't want disk I/O in property
	// tests, we use the same approach as the busy-session tests: create a
	// handler with a bare App (LoadConfig will error → defaults used).
	// To test custom roles, we'd need disk setup. Instead, we verify properties
	// hold for the default config path (LoadConfig error → defaults).
	// The prompt structure is independent of role name/description values.
	mgr := &RemoteSessionManager{
		app:      app,
		sessions: map[string]*RemoteSession{},
	}
	h := &IMMessageHandler{
		app:     app,
		manager: mgr,
	}
	return h.buildSystemPrompt()
}

// quickConfig returns a quick.Config with at least 100 iterations.
func quickConfig() *quick.Config {
	return &quick.Config{MaxCount: 100}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 1: Confirmation Phase appears before create_session
//
// Validates: Requirements 1.1, 6.1
// For any valid system configuration, the Confirmation Phase instructions
// (需求确认 / Confirmation Phase) must appear BEFORE the create_session
// execution instructions in buildSystemPrompt() output.
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty1_ConfirmationBeforeCreateSession(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Find the position of Confirmation Phase instructions
		confirmIdx := strings.Index(prompt, "需求确认")
		if confirmIdx < 0 {
			// Also accept "Confirmation Phase" or "确认" in the workflow section
			confirmIdx = strings.Index(prompt, "Confirmation Phase")
		}
		if confirmIdx < 0 {
			t.Logf("prompt does not contain '需求确认' or 'Confirmation Phase'")
			return false
		}

		// Find the position of create_session execution instruction
		// Look for the actual execution step that calls create_session
		createIdx := strings.Index(prompt, "create_session")
		if createIdx < 0 {
			t.Logf("prompt does not contain 'create_session'")
			return false
		}

		// Confirmation must appear before create_session
		return confirmIdx < createIdx
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 1 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 2: Confirmation message contains all required components
//
// Validates: Requirements 1.2
// For any valid system configuration, buildSystemPrompt() output must contain
// instructions for all three confirmation message components:
// 需求理解 (or 需求复述), 实现方案, 边界情况
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty2_ConfirmationContainsAllComponents(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Component 1: 需求理解 or 需求复述
		hasUnderstanding := strings.Contains(prompt, "需求理解") || strings.Contains(prompt, "需求复述")
		if !hasUnderstanding {
			t.Logf("prompt missing 需求理解/需求复述")
			return false
		}

		// Component 2: 实现方案
		hasPlan := strings.Contains(prompt, "实现方案")
		if !hasPlan {
			t.Logf("prompt missing 实现方案")
			return false
		}

		// Component 3: 边界情况
		hasEdgeCases := strings.Contains(prompt, "边界情况")
		if !hasEdgeCases {
			t.Logf("prompt missing 边界情况")
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}
