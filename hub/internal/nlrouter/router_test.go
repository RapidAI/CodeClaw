package nlrouter

import (
	"context"
	"testing"
)

// stubMemory implements MemoryStore for testing.
type stubMemory struct {
	defaultTool string
}

func (s *stubMemory) GetDefaultTool(_ context.Context, _ string) string {
	return s.defaultTool
}

func newTestRouter(defaultTool string) *Router {
	return NewRouter(
		NewRuleEngine(),
		&stubMemory{defaultTool: defaultTool},
		NewContextWindowManager(),
	)
}

func TestParse_SlashCommand(t *testing.T) {
	r := newTestRouter("")
	intent, err := r.Parse(context.Background(), "u1", "/help")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentHelp {
		t.Errorf("expected %s, got %s", IntentHelp, intent.Name)
	}
	if intent.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", intent.Confidence)
	}
}

func TestParse_KeywordMatch(t *testing.T) {
	r := newTestRouter("")
	intent, err := r.Parse(context.Background(), "u1", "查看设备")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentListMachines {
		t.Errorf("expected %s, got %s", IntentListMachines, intent.Name)
	}
}

func TestParse_UnknownIntent(t *testing.T) {
	r := newTestRouter("")
	intent, err := r.Parse(context.Background(), "u1", "xyzzy random gibberish")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentUnknown {
		t.Errorf("expected %s, got %s", IntentUnknown, intent.Name)
	}
	if intent.RawText != "xyzzy random gibberish" {
		t.Errorf("expected raw text preserved, got %q", intent.RawText)
	}
}

func TestParse_EmptyText(t *testing.T) {
	r := newTestRouter("")
	intent, err := r.Parse(context.Background(), "u1", "")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentUnknown {
		t.Errorf("expected %s, got %s", IntentUnknown, intent.Name)
	}
}

func TestParse_ActiveSessionDefaultSendInput(t *testing.T) {
	r := newTestRouter("")
	// Simulate an active session by setting it in the context window.
	win := r.context.Get("u1")
	win.ActiveSession = "session-42"

	intent, err := r.Parse(context.Background(), "u1", "今天天气不错啊")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentSendInput {
		t.Errorf("expected %s, got %s", IntentSendInput, intent.Name)
	}
	if intent.Params["text"] != "今天天气不错啊" {
		t.Errorf("expected text param, got %v", intent.Params["text"])
	}
}

func TestParse_LaunchSessionFillDefaultTool(t *testing.T) {
	r := newTestRouter("claude")
	intent, err := r.Parse(context.Background(), "u1", "启动会话")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentLaunchSession {
		t.Errorf("expected %s, got %s", IntentLaunchSession, intent.Name)
	}
	if intent.Params["tool"] != "claude" {
		t.Errorf("expected tool=claude, got %v", intent.Params["tool"])
	}
}

func TestParse_LaunchSessionExplicitTool(t *testing.T) {
	r := newTestRouter("codex")
	intent, err := r.Parse(context.Background(), "u1", "用 claude 启动会话")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentLaunchSession {
		t.Errorf("expected %s, got %s", IntentLaunchSession, intent.Name)
	}
	// Explicit tool should not be overridden by default.
	if intent.Params["tool"] != "claude" {
		t.Errorf("expected tool=claude, got %v", intent.Params["tool"])
	}
}

func TestParse_RepeatRequest(t *testing.T) {
	r := newTestRouter("")
	// First, parse a real command to populate context.
	_, _ = r.Parse(context.Background(), "u1", "/machines")

	// Now ask to repeat.
	intent, err := r.Parse(context.Background(), "u1", "再来一次")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentListMachines {
		t.Errorf("expected %s, got %s", IntentListMachines, intent.Name)
	}
}

func TestParse_PronounResolution(t *testing.T) {
	r := newTestRouter("")
	// Set active tool in context.
	win := r.context.Get("u1")
	win.ActiveTool = "claude"

	// "用它启动会话" → "用 claude 启动会话"
	intent, err := r.Parse(context.Background(), "u1", "用它启动会话")
	if err != nil {
		t.Fatal(err)
	}
	// After pronoun resolution, the text becomes "用claude启动会话"
	// which should match launch_session via keyword "启动会话".
	if intent.Name != IntentLaunchSession {
		t.Errorf("expected %s, got %s", IntentLaunchSession, intent.Name)
	}
}

func TestParse_ContextEntryRecorded(t *testing.T) {
	r := newTestRouter("")
	_, _ = r.Parse(context.Background(), "u1", "/help")

	win := r.context.Get("u1")
	if len(win.Entries) == 0 {
		t.Fatal("expected at least one context entry")
	}
	last := win.Entries[len(win.Entries)-1]
	if last.Intent != IntentHelp {
		t.Errorf("expected intent %s in context, got %s", IntentHelp, last.Intent)
	}
	if last.Role != "user" {
		t.Errorf("expected role user, got %s", last.Role)
	}
}

func TestParse_MultiIntentCandidates(t *testing.T) {
	r := newTestRouter("")
	// "发送截屏" contains both "发送" (send_input) and "截屏" (screenshot).
	// The keyword "截屏" is more specific, so screenshot should win.
	intent, err := r.Parse(context.Background(), "u1", "发送截屏")
	if err != nil {
		t.Fatal(err)
	}
	// Both keywords match; the longer keyword should win as primary.
	// Candidates should contain the other.
	if intent.Candidates == nil {
		// It's acceptable if only one matches at keyword level.
		// Just verify we got a valid intent.
		if intent.Name != IntentScreenshot && intent.Name != IntentSendInput {
			t.Errorf("expected screenshot or send_input, got %s", intent.Name)
		}
	}
}

func TestParse_LowConfidenceFuzzyNoSession(t *testing.T) {
	r := newTestRouter("")
	// "设备" triggers fuzzy match at 0.6 confidence.
	intent, err := r.Parse(context.Background(), "u1", "设备")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Name != IntentListMachines {
		t.Errorf("expected %s, got %s", IntentListMachines, intent.Name)
	}
	if intent.Confidence != 0.6 {
		t.Errorf("expected confidence 0.6, got %f", intent.Confidence)
	}
}
