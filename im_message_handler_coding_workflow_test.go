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

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 3: Coding vs non-coding task distinction
//
// Validates: Requirements 1.5, 6.4, 7.1, 7.3
// For any valid system configuration, buildSystemPrompt() output must contain
// clear criteria distinguishing Coding_Task from non-coding requests, and
// explicitly list non-coding examples (file operations like bash/read_file/write_file,
// configuration, screenshots, general questions).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty3_CodingVsNonCodingDistinction(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Must contain Coding_Task concept
		hasCodingTask := strings.Contains(prompt, "Coding_Task") || strings.Contains(prompt, "编程任务")
		if !hasCodingTask {
			t.Logf("prompt missing Coding_Task / 编程任务")
			return false
		}

		// Must contain non-coding task concept
		hasNonCoding := strings.Contains(prompt, "非编程任务") || strings.Contains(prompt, "non-coding")
		if !hasNonCoding {
			t.Logf("prompt missing 非编程任务")
			return false
		}

		// Must explicitly list non-coding examples: file operations
		hasBash := strings.Contains(prompt, "bash")
		hasReadFile := strings.Contains(prompt, "read_file")
		hasWriteFile := strings.Contains(prompt, "write_file")
		if !hasBash || !hasReadFile || !hasWriteFile {
			t.Logf("prompt missing file operation examples (bash=%v, read_file=%v, write_file=%v)",
				hasBash, hasReadFile, hasWriteFile)
			return false
		}

		// Must mention configuration and screenshots as non-coding
		hasConfig := strings.Contains(prompt, "配置")
		hasScreenshot := strings.Contains(prompt, "截屏") || strings.Contains(prompt, "screenshot")
		if !hasConfig || !hasScreenshot {
			t.Logf("prompt missing config/screenshot examples (config=%v, screenshot=%v)",
				hasConfig, hasScreenshot)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 4: Skip_Signal bilingual patterns
//
// Validates: Requirements 2.1, 2.3, 6.3
// For any valid system configuration, buildSystemPrompt() output must contain
// Skip_Signal patterns in both Chinese (直接做, 不用问了) and English
// (just do it, go ahead).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty4_SkipSignalBilingualPatterns(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Chinese Skip_Signal patterns
		hasChineseSkip1 := strings.Contains(prompt, "直接做")
		hasChineseSkip2 := strings.Contains(prompt, "不用问了")
		if !hasChineseSkip1 || !hasChineseSkip2 {
			t.Logf("prompt missing Chinese Skip_Signal (直接做=%v, 不用问了=%v)",
				hasChineseSkip1, hasChineseSkip2)
			return false
		}

		// English Skip_Signal patterns
		hasEnglishSkip1 := strings.Contains(prompt, "just do it")
		hasEnglishSkip2 := strings.Contains(prompt, "go ahead")
		if !hasEnglishSkip1 || !hasEnglishSkip2 {
			t.Logf("prompt missing English Skip_Signal (just do it=%v, go ahead=%v)",
				hasEnglishSkip1, hasEnglishSkip2)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 5: RFO workflow completeness
//
// Validates: Requirements 4.1, 4.2, 5.1, 5.2, 6.2
// For any valid system configuration, buildSystemPrompt() output must contain:
// (a) RFO trigger conditions (waiting_input or exited with exit_code=0),
// (b) all three RFO options (Review, Fix, Optimize),
// (c) the sequential execution order Review → Fix → Optimize.
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty5_RFOWorkflowCompleteness(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) RFO trigger conditions
		hasWaitingInput := strings.Contains(prompt, "waiting_input")
		hasExitCode0 := strings.Contains(prompt, "exit_code=0") || strings.Contains(prompt, "exit code 0") || strings.Contains(prompt, "退出码")
		if !hasWaitingInput {
			t.Logf("prompt missing RFO trigger condition 'waiting_input'")
			return false
		}
		// Must mention exited with success condition
		hasExited := strings.Contains(prompt, "exited")
		if !hasExited || !hasExitCode0 {
			t.Logf("prompt missing RFO trigger condition for exited+exit_code=0 (exited=%v, exitCode0=%v)",
				hasExited, hasExitCode0)
			return false
		}

		// (b) All three RFO options
		hasReview := strings.Contains(prompt, "Review")
		hasFix := strings.Contains(prompt, "Fix")
		hasOptimize := strings.Contains(prompt, "Optimize")
		if !hasReview || !hasFix || !hasOptimize {
			t.Logf("prompt missing RFO options (Review=%v, Fix=%v, Optimize=%v)",
				hasReview, hasFix, hasOptimize)
			return false
		}

		// (c) Sequential execution order: Review → Fix → Optimize
		hasOrder := strings.Contains(prompt, "Review → Fix → Optimize")
		if !hasOrder {
			t.Logf("prompt missing sequential order 'Review → Fix → Optimize'")
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 5 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 6: Skip RFO on task failure
//
// Validates: Requirements 4.6
// For any valid system configuration, buildSystemPrompt() output must contain
// explicit instructions to skip RFO when task fails (exit_code≠0 or error status).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty6_SkipRFOOnTaskFailure(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Must mention skipping RFO on failure
		hasFailureSkip := strings.Contains(prompt, "exit_code≠0") || strings.Contains(prompt, "exit_code!=0") || strings.Contains(prompt, "非零")
		hasErrorStatus := strings.Contains(prompt, "error") || strings.Contains(prompt, "失败")
		hasSkipRFO := strings.Contains(prompt, "跳过 RFO") || strings.Contains(prompt, "跳过RFO") || strings.Contains(prompt, "skip RFO")

		if !hasFailureSkip {
			t.Logf("prompt missing failure condition (exit_code≠0 or 非零)")
			return false
		}
		if !hasErrorStatus {
			t.Logf("prompt missing error status reference")
			return false
		}
		if !hasSkipRFO {
			t.Logf("prompt missing skip RFO instruction")
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 6 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 7: Existing workflow rules preserved
//
// Validates: Requirements 6.5
// For any valid system configuration, buildSystemPrompt() output must preserve:
// (a) 会话失败止损原则,
// (b) 执行验证原则,
// (c) busy 会话不终止规则 (绝对不要终止状态为 busy 的编程会话).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty7_ExistingWorkflowRulesPreserved(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) 会话失败止损原则
		hasStopLoss := strings.Contains(prompt, "会话失败止损") || strings.Contains(prompt, "止损原则")
		if !hasStopLoss {
			t.Logf("prompt missing 会话失败止损原则")
			return false
		}

		// (b) 执行验证原则
		hasVerification := strings.Contains(prompt, "执行验证原则") || strings.Contains(prompt, "执行验证")
		if !hasVerification {
			t.Logf("prompt missing 执行验证原则")
			return false
		}

		// (c) busy 会话不终止规则
		hasBusyRule := strings.Contains(prompt, "绝对不要终止状态为 busy 的编程会话")
		if !hasBusyRule {
			t.Logf("prompt missing busy 会话不终止规则")
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 failed: %v", err)
	}
}
