package im

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/nlrouter"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type mockPlugin struct {
	name         string
	caps         CapabilityDeclaration
	sentTexts    []string
	sentCards    []OutgoingMessage
	handler      func(msg IncomingMessage)
	mu           sync.Mutex
}

func (m *mockPlugin) Name() string                          { return m.name }
func (m *mockPlugin) Start(_ context.Context) error         { return nil }
func (m *mockPlugin) Stop(_ context.Context) error          { return nil }
func (m *mockPlugin) Capabilities() CapabilityDeclaration   { return m.caps }
func (m *mockPlugin) ResolveUser(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockPlugin) SendImage(_ context.Context, _ UserTarget, _ string, _ string) error { return nil }

func (m *mockPlugin) ReceiveMessage(handler func(msg IncomingMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

func (m *mockPlugin) SendText(_ context.Context, _ UserTarget, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func (m *mockPlugin) SendCard(_ context.Context, _ UserTarget, card OutgoingMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentCards = append(m.sentCards, card)
	return nil
}

type mockRouter struct {
	parseFunc func(ctx context.Context, userID, text string) (*nlrouter.Intent, error)
}

func (m *mockRouter) Parse(ctx context.Context, userID, text string) (*nlrouter.Intent, error) {
	if m.parseFunc != nil {
		return m.parseFunc(ctx, userID, text)
	}
	return &nlrouter.Intent{Name: nlrouter.IntentHelp, Confidence: 1.0, Params: map[string]interface{}{}}, nil
}

type mockExecutor struct {
	executeFunc func(ctx context.Context, userID string, intent *nlrouter.Intent) (*GenericResponse, error)
}

func (m *mockExecutor) Execute(ctx context.Context, userID string, intent *nlrouter.Intent) (*GenericResponse, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, userID, intent)
	}
	return &GenericResponse{StatusCode: 200, StatusIcon: "✅", Title: "OK", Body: "done"}, nil
}

type mockIdentity struct {
	resolveFunc func(ctx context.Context, platform, uid string) (string, error)
}

func (m *mockIdentity) ResolveUser(ctx context.Context, platform, uid string) (string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, platform, uid)
	}
	return "unified_" + uid, nil
}

// helper to build a basic adapter with a registered plugin.
func setupAdapter(plugin *mockPlugin) (*Adapter, *mockRouter, *mockExecutor) {
	router := &mockRouter{}
	executor := &mockExecutor{}
	identity := &mockIdentity{}
	adapter := NewAdapter(router, executor, identity)
	_ = adapter.RegisterPlugin(plugin)
	return adapter, router, executor
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegisterPlugin_Success(t *testing.T) {
	adapter := NewAdapter(&mockRouter{}, &mockExecutor{}, &mockIdentity{})
	plugin := &mockPlugin{name: "test"}
	if err := adapter.RegisterPlugin(plugin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := adapter.GetPlugin("test"); got == nil {
		t.Fatal("expected plugin to be registered")
	}
}

func TestRegisterPlugin_EmptyName(t *testing.T) {
	adapter := NewAdapter(&mockRouter{}, &mockExecutor{}, &mockIdentity{})
	plugin := &mockPlugin{name: ""}
	if err := adapter.RegisterPlugin(plugin); err == nil {
		t.Fatal("expected error for empty plugin name")
	}
}

func TestRegisterPlugin_Duplicate(t *testing.T) {
	adapter := NewAdapter(&mockRouter{}, &mockExecutor{}, &mockIdentity{})
	_ = adapter.RegisterPlugin(&mockPlugin{name: "dup"})
	if err := adapter.RegisterPlugin(&mockPlugin{name: "dup"}); err == nil {
		t.Fatal("expected error for duplicate plugin")
	}
}

func TestInjectionDetection(t *testing.T) {
	cases := []struct {
		text    string
		blocked bool
	}{
		{"hello world", false},
		{"查看设备", false},
		{"rm -rf /; echo pwned", true},
		{"cat file | grep x", true},
		{"foo && bar", true},
		{"echo `whoami`", true},
		{"$(id)", true},
		{"${HOME}", true},
		{"normal text", false},
	}
	for _, tc := range cases {
		got := containsInjection(tc.text)
		if got != tc.blocked {
			t.Errorf("containsInjection(%q) = %v, want %v", tc.text, got, tc.blocked)
		}
	}
}

func TestRateLimiter_AllowsUpToMax(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < rateLimitMaxTokens; i++ {
		if !rl.allow("user1") {
			t.Fatalf("expected allow at request %d", i+1)
		}
	}
	// 31st request should be denied.
	if rl.allow("user1") {
		t.Fatal("expected rate limit to deny 31st request")
	}
}

func TestRateLimiter_RefillsAfterInterval(t *testing.T) {
	rl := newRateLimiter()
	// Exhaust tokens.
	for i := 0; i < rateLimitMaxTokens; i++ {
		rl.allow("user1")
	}
	// Manually set refillAt to the past.
	rl.mu.Lock()
	rl.buckets["user1"].refillAt = time.Now().Add(-1 * time.Second)
	rl.mu.Unlock()

	if !rl.allow("user1") {
		t.Fatal("expected allow after refill")
	}
}

func TestRateLimiter_IndependentUsers(t *testing.T) {
	rl := newRateLimiter()
	// Exhaust user1.
	for i := 0; i < rateLimitMaxTokens; i++ {
		rl.allow("user1")
	}
	// user2 should still be allowed.
	if !rl.allow("user2") {
		t.Fatal("expected user2 to be allowed independently")
	}
}

func TestHandleMessage_IdentityFailure(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	identity := &mockIdentity{
		resolveFunc: func(_ context.Context, _, _ string) (string, error) {
			return "", fmt.Errorf("unbound user")
		},
	}
	adapter := NewAdapter(&mockRouter{}, &mockExecutor{}, identity)
	_ = adapter.RegisterPlugin(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected error response")
	}
	if got := plugin.sentTexts[0]; !contains(got, "身份验证失败") {
		t.Fatalf("unexpected response: %s", got)
	}
}

func TestHandleMessage_InjectionBlocked(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	adapter, _, _ := setupAdapter(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "rm -rf /; echo pwned",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected injection blocked response")
	}
	if !contains(plugin.sentTexts[0], "不安全字符") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_RateLimited(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	adapter, _, _ := setupAdapter(plugin)

	// Exhaust rate limit for unified_uid1.
	adapter.limiter.mu.Lock()
	adapter.limiter.buckets["unified_uid1"] = &rateBucket{
		tokens:   0,
		refillAt: time.Now().Add(1 * time.Minute),
	}
	adapter.limiter.mu.Unlock()

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "hello",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected rate limit response")
	}
	if !contains(plugin.sentTexts[0], "请求过于频繁") {
		t.Fatalf("unexpected response: %s", plugin.sentTexts[0])
	}
}

func TestHandleMessage_NormalFlow_SendCard(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: true}}
	adapter, _, _ := setupAdapter(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "help",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentCards) == 0 {
		t.Fatal("expected card response for rich-card-capable plugin")
	}
}

func TestHandleMessage_NormalFlow_SendText(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	adapter, _, _ := setupAdapter(plugin)

	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "help",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) == 0 {
		t.Fatal("expected text response for text-only plugin")
	}
}

func TestHandleMessage_HighRiskConfirmation(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	router := &mockRouter{
		parseFunc: func(_ context.Context, _, _ string) (*nlrouter.Intent, error) {
			return &nlrouter.Intent{
				Name:       nlrouter.IntentKillSession,
				Confidence: 1.0,
				Params:     map[string]interface{}{"session": "1"},
			}, nil
		},
	}
	executor := &mockExecutor{}
	identity := &mockIdentity{}
	adapter := NewAdapter(router, executor, identity)
	_ = adapter.RegisterPlugin(plugin)

	// Send kill_session request — should get confirmation prompt.
	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "终止会话 1",
	})

	plugin.mu.Lock()
	if len(plugin.sentTexts) != 1 || !contains(plugin.sentTexts[0], "高风险操作确认") {
		t.Fatalf("expected confirmation prompt, got: %v", plugin.sentTexts)
	}
	plugin.mu.Unlock()

	// Confirm with "确认".
	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test",
		PlatformUID:  "uid1",
		Text:         "确认",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.sentTexts) < 2 {
		t.Fatal("expected execution response after confirmation")
	}
}

func TestHandleMessage_HighRiskConfirmation_Cancel(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	router := &mockRouter{
		parseFunc: func(_ context.Context, _, _ string) (*nlrouter.Intent, error) {
			return &nlrouter.Intent{
				Name:       nlrouter.IntentKillSession,
				Confidence: 1.0,
				Params:     map[string]interface{}{},
			}, nil
		},
	}
	adapter := NewAdapter(router, &mockExecutor{}, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	// Trigger confirmation.
	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test", PlatformUID: "uid1", Text: "kill session",
	})

	// Reply with non-confirmation text.
	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test", PlatformUID: "uid1", Text: "no",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	found := false
	for _, t := range plugin.sentTexts {
		if contains(t, "操作已取消") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cancellation message, got: %v", plugin.sentTexts)
	}
}

func TestHandleMessage_HighRiskConfirmation_Timeout(t *testing.T) {
	plugin := &mockPlugin{name: "test", caps: CapabilityDeclaration{SupportsRichCard: false}}
	router := &mockRouter{
		parseFunc: func(_ context.Context, _, _ string) (*nlrouter.Intent, error) {
			return &nlrouter.Intent{
				Name:       nlrouter.IntentKillSession,
				Confidence: 1.0,
				Params:     map[string]interface{}{},
			}, nil
		},
	}
	adapter := NewAdapter(router, &mockExecutor{}, &mockIdentity{})
	_ = adapter.RegisterPlugin(plugin)

	// Trigger confirmation.
	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test", PlatformUID: "uid1", Text: "kill",
	})

	// Expire the confirmation manually.
	adapter.confirmMu.Lock()
	if p, ok := adapter.confirmations["unified_uid1"]; ok {
		p.ExpiresAt = time.Now().Add(-1 * time.Second)
	}
	adapter.confirmMu.Unlock()

	// Any reply should trigger timeout.
	adapter.HandleMessage(context.Background(), IncomingMessage{
		PlatformName: "test", PlatformUID: "uid1", Text: "确认",
	})

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	found := false
	for _, t := range plugin.sentTexts {
		if contains(t, "确认超时") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected timeout message, got: %v", plugin.sentTexts)
	}
}

func TestIsHighRiskIntent(t *testing.T) {
	cases := []struct {
		intent *nlrouter.Intent
		risk   bool
	}{
		{&nlrouter.Intent{Name: nlrouter.IntentKillSession}, true},
		{&nlrouter.Intent{Name: nlrouter.IntentLaunchSession, Params: map[string]interface{}{"prompt": "run deploy.sh"}}, true},
		{&nlrouter.Intent{Name: nlrouter.IntentLaunchSession, Params: map[string]interface{}{}}, false},
		{&nlrouter.Intent{Name: nlrouter.IntentHelp}, false},
		{&nlrouter.Intent{Name: nlrouter.IntentListMachines}, false},
	}
	for _, tc := range cases {
		if got := isHighRiskIntent(tc.intent); got != tc.risk {
			t.Errorf("isHighRiskIntent(%s) = %v, want %v", tc.intent.Name, got, tc.risk)
		}
	}
}

func TestTruncateAtLine(t *testing.T) {
	text := "line1\nline2\nline3\nline4"
	result := truncateAtLine(text, 15)
	if len(result) > 15+5 { // some tolerance for the ellipsis
		t.Fatalf("truncated text too long: %q", result)
	}
	if !contains(result, "…") {
		t.Fatalf("expected ellipsis in truncated text: %q", result)
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
