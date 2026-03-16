package nlrouter

import (
	"testing"
)

func TestNewRuleEngine(t *testing.T) {
	re := NewRuleEngine()
	if re == nil {
		t.Fatal("NewRuleEngine returned nil")
	}
	if len(re.slashCommands) == 0 {
		t.Error("slash commands not initialized")
	}
	if len(re.keywordRules) == 0 {
		t.Error("keyword rules not initialized")
	}
}

// --- Slash command tests ---

func TestSlashHelp(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/help")
	assertIntent(t, intent, IntentHelp, 1.0)
}

func TestSlashMachines(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/machines")
	assertIntent(t, intent, IntentListMachines, 1.0)
}

func TestSlashSessions(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/sessions")
	assertIntent(t, intent, IntentListSessions, 1.0)
}

func TestSlashUseWithID(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/use 42")
	assertIntent(t, intent, IntentUseSession, 1.0)
	assertParam(t, intent, "id", "42")
}

func TestSlashUseWithoutID(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/use")
	assertIntent(t, intent, IntentUseSession, 1.0)
}

func TestSlashExit(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/exit")
	assertIntent(t, intent, IntentExitSession, 1.0)
}

func TestSlashKillWithID(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/kill abc123")
	assertIntent(t, intent, IntentKillSession, 1.0)
	assertParam(t, intent, "id", "abc123")
}

func TestSlashScreenshot(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/screenshot")
	assertIntent(t, intent, IntentScreenshot, 1.0)
}

func TestSlashMemory(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/memory")
	assertIntent(t, intent, IntentViewMemory, 1.0)
}

func TestSlashClearMemory(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/clear_memory")
	assertIntent(t, intent, IntentClearMemory, 1.0)
}

func TestSlashUnknown(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("/foobar")
	if intent != nil {
		t.Errorf("expected nil for unknown slash command, got %v", intent.Name)
	}
}

// --- Chinese keyword tests ---

func TestKeywordListMachinesCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("查看设备")
	assertIntent(t, intent, IntentListMachines, 0.8)
}

func TestKeywordListSessionsCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("查看会话")
	assertIntent(t, intent, IntentListSessions, 0.8)
}

func TestKeywordLaunchSessionCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("启动会话")
	assertIntent(t, intent, IntentLaunchSession, 0.8)
}

func TestKeywordKillCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("终止会话 3")
	assertIntent(t, intent, IntentKillSession, 0.8)
}

func TestKeywordScreenshotCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("截屏")
	assertIntent(t, intent, IntentScreenshot, 0.8)
}

func TestKeywordHelpCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("帮助")
	assertIntent(t, intent, IntentHelp, 0.8)
}

func TestKeywordCrystallizeCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("保存技能")
	assertIntent(t, intent, IntentCrystallizeSkill, 0.8)
}

func TestKeywordViewMemoryCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("查看记忆")
	assertIntent(t, intent, IntentViewMemory, 0.8)
}

func TestKeywordClearMemoryCN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("清除记忆")
	assertIntent(t, intent, IntentClearMemory, 0.8)
}

// --- English keyword tests ---

func TestKeywordListMachinesEN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("list machines")
	assertIntent(t, intent, IntentListMachines, 0.8)
}

func TestKeywordShowSessionsEN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("show sessions")
	assertIntent(t, intent, IntentListSessions, 0.8)
}

func TestKeywordStartSessionEN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("start session with claude")
	assertIntent(t, intent, IntentLaunchSession, 0.8)
}

func TestKeywordCallToolEN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("call tool weather")
	assertIntent(t, intent, IntentCallMCPTool, 0.8)
}

func TestKeywordRunSkillEN(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("run skill deploy")
	assertIntent(t, intent, IntentRunSkill, 0.8)
}

// --- Parameter extraction tests ---

func TestExtractToolName(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("用 claude 启动会话")
	assertIntent(t, intent, IntentLaunchSession, 0.8)
	assertParam(t, intent, "tool", "claude")
}

func TestExtractToolNameWith(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("launch with gemini")
	assertIntent(t, intent, IntentLaunchSession, 0.8)
	assertParam(t, intent, "tool", "gemini")
}

func TestExtractPath(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("启动会话 用 claude ~/my-project")
	assertIntent(t, intent, IntentLaunchSession, 0.8)
	assertParam(t, intent, "path", "~/my-project")
}

func TestExtractSessionID(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("切换会话 3")
	assertIntent(t, intent, IntentUseSession, 0.8)
	assertParam(t, intent, "id", "3")
}

// --- Fuzzy matching tests ---

func TestFuzzyMachine(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("我的设备呢")
	assertIntent(t, intent, IntentListMachines, 0.6)
}

func TestFuzzySession(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("看看会话")
	// "查看会话" is not present, but "会话" partial match should trigger keyword "查看会话" won't match,
	// but fuzzy "会话" should match at 0.6
	// Actually "会话" is in the keyword "查看会话" — let's check: "看看会话" contains "会话" but not "查看会话"
	// So keyword won't match, fuzzy "会话" will match
	assertIntent(t, intent, IntentListSessions, 0.6)
}

func TestFuzzyHelp(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("帮我看看")
	assertIntent(t, intent, IntentHelp, 0.6)
}

// --- Edge cases ---

func TestEmptyInput(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("")
	if intent != nil {
		t.Error("expected nil for empty input")
	}
}

func TestWhitespaceOnly(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("   ")
	if intent != nil {
		t.Error("expected nil for whitespace-only input")
	}
}

func TestNoMatch(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("今天天气怎么样")
	if intent != nil {
		t.Errorf("expected nil for unrelated input, got %v", intent.Name)
	}
}

func TestCaseInsensitive(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("LIST MACHINES")
	assertIntent(t, intent, IntentListMachines, 0.8)
}

func TestSlashCommandWithLeadingSpace(t *testing.T) {
	re := NewRuleEngine()
	intent := re.Match("  /help")
	assertIntent(t, intent, IntentHelp, 1.0)
}

// --- Helpers ---

func assertIntent(t *testing.T, intent *Intent, expectedName string, expectedConfidence float64) {
	t.Helper()
	if intent == nil {
		t.Fatalf("expected intent %q, got nil", expectedName)
	}
	if intent.Name != expectedName {
		t.Errorf("expected intent name %q, got %q", expectedName, intent.Name)
	}
	if intent.Confidence != expectedConfidence {
		t.Errorf("expected confidence %v, got %v", expectedConfidence, intent.Confidence)
	}
}

func assertParam(t *testing.T, intent *Intent, key, expected string) {
	t.Helper()
	if intent == nil {
		t.Fatalf("intent is nil, cannot check param %q", key)
	}
	val, ok := intent.Params[key]
	if !ok {
		t.Errorf("expected param %q not found in %v", key, intent.Params)
		return
	}
	if val != expected {
		t.Errorf("expected param %q = %q, got %q", key, expected, val)
	}
}
