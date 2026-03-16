package im

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/nlrouter"
)

// ---------------------------------------------------------------------------
// Abstraction interfaces (avoid circular imports)
// ---------------------------------------------------------------------------

// IntentRouter abstracts the NL Router to avoid circular imports.
type IntentRouter interface {
	Parse(ctx context.Context, userID, text string) (*nlrouter.Intent, error)
}

// IntentExecutor abstracts the intent execution bridge.
type IntentExecutor interface {
	Execute(ctx context.Context, userID string, intent *nlrouter.Intent) (*GenericResponse, error)
}

// IdentityResolver abstracts the Identity_Service for user mapping.
type IdentityResolver interface {
	ResolveUser(ctx context.Context, platformName, platformUID string) (string, error)
}

// ---------------------------------------------------------------------------
// Shell injection detection
// ---------------------------------------------------------------------------

// shellMetaChars are patterns that indicate potential shell injection.
var shellMetaChars = []string{";", "|", "&&", "`", "$(", "${"}

// containsInjection returns true if text contains any shell metacharacter.
func containsInjection(text string) bool {
	for _, mc := range shellMetaChars {
		if strings.Contains(text, mc) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rate limiter (token-bucket, 30 tokens/min per user)
// ---------------------------------------------------------------------------

const (
	rateLimitMaxTokens = 30
	rateLimitRefill    = time.Minute
)

// rateBucket is a simple per-user token bucket.
type rateBucket struct {
	tokens   int
	refillAt time.Time
}

// rateLimiter manages per-user rate limiting.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*rateBucket)}
}

// allow returns true if the user has remaining tokens. It refills the bucket
// if the refill interval has elapsed.
func (rl *rateLimiter) allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[userID]
	now := time.Now()
	if !ok {
		rl.buckets[userID] = &rateBucket{
			tokens:   rateLimitMaxTokens - 1,
			refillAt: now.Add(rateLimitRefill),
		}
		return true
	}

	// Refill if interval elapsed.
	if now.After(b.refillAt) {
		b.tokens = rateLimitMaxTokens
		b.refillAt = now.Add(rateLimitRefill)
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// ---------------------------------------------------------------------------
// High-risk confirmation
// ---------------------------------------------------------------------------

// PendingConfirmation represents a high-risk operation awaiting user confirmation.
type PendingConfirmation struct {
	ID        string
	UserID    string
	Intent    *nlrouter.Intent
	ExpiresAt time.Time
	Confirmed bool
}

// confirmationTokens that the user can reply with to confirm.
var confirmationTokens = []string{"确认", "yes", "1"}

// isConfirmationReply checks if text is a confirmation reply.
func isConfirmationReply(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, tok := range confirmationTokens {
		if lower == tok {
			return true
		}
	}
	return false
}

// isHighRiskIntent returns true for intents that require user confirmation.
func isHighRiskIntent(intent *nlrouter.Intent) bool {
	if intent.Name == nlrouter.IntentKillSession {
		return true
	}
	if intent.Name == nlrouter.IntentLaunchSession {
		// High risk if the launch contains a system command (prompt param).
		if prompt, ok := intent.Params["prompt"]; ok {
			if s, ok := prompt.(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Adapter — the IM Adapter Core
// ---------------------------------------------------------------------------

// Adapter is the central IM adapter that manages registered IM plugins,
// routes incoming messages through identity mapping, rate limiting,
// injection detection, NL Router parsing, intent execution, and
// response formatting.
type Adapter struct {
	mu       sync.RWMutex
	plugins  map[string]IMPlugin

	router   IntentRouter
	executor IntentExecutor
	identity IdentityResolver

	limiter      *rateLimiter
	confirmMu    sync.Mutex
	confirmations map[string]*PendingConfirmation // userID → pending
}

// NewAdapter creates a new IM Adapter with the given dependencies.
func NewAdapter(router IntentRouter, executor IntentExecutor, identity IdentityResolver) *Adapter {
	return &Adapter{
		plugins:       make(map[string]IMPlugin),
		router:        router,
		executor:      executor,
		identity:      identity,
		limiter:       newRateLimiter(),
		confirmations: make(map[string]*PendingConfirmation),
	}
}

// SetIdentityResolver replaces the identity resolver after construction.
// This is useful when the resolver depends on the adapter itself (e.g.
// PluginIdentityResolver).
func (a *Adapter) SetIdentityResolver(resolver IdentityResolver) {
	a.identity = resolver
}

// RegisterPlugin registers an IM plugin with the adapter.
// It validates that the plugin implements all required interface methods
// by checking that Name() returns a non-empty string.
func (a *Adapter) RegisterPlugin(plugin IMPlugin) error {
	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("im: plugin Name() returned empty string, refusing to register")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.plugins[name]; exists {
		return fmt.Errorf("im: plugin %q already registered", name)
	}

	// Wire the message handler so the plugin routes messages to us.
	plugin.ReceiveMessage(func(msg IncomingMessage) {
		a.HandleMessage(context.Background(), msg)
	})

	a.plugins[name] = plugin
	log.Printf("[IM Adapter] registered plugin: %s", name)
	return nil
}

// GetPlugin returns the registered plugin by name, or nil.
func (a *Adapter) GetPlugin(name string) IMPlugin {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.plugins[name]
}

// HandleMessage is the main entry point called by IM plugins when they
// receive a message. It orchestrates the full pipeline:
//
//  1. Identity mapping (platformUID → unifiedUserID)
//  2. Rate limiting (30 req/min per user)
//  3. Pending confirmation check
//  4. Injection detection
//  5. NL Router intent parsing
//  6. High-risk confirmation prompt (if needed)
//  7. Intent execution
//  8. Response formatting & delivery based on CapabilityDeclaration
func (a *Adapter) HandleMessage(ctx context.Context, msg IncomingMessage) {
	plugin := a.GetPlugin(msg.PlatformName)
	if plugin == nil {
		log.Printf("[IM Adapter] no plugin registered for platform %q", msg.PlatformName)
		return
	}

	target := UserTarget{PlatformUID: msg.PlatformUID}

	// 1. Identity mapping
	unifiedID, err := a.identity.ResolveUser(ctx, msg.PlatformName, msg.PlatformUID)
	if err != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 403,
			StatusIcon: "🔒",
			Title:      "身份验证失败",
			Body:       fmt.Sprintf("无法识别您的身份，请先完成绑定。\n错误: %s", err.Error()),
		})
		return
	}
	msg.UnifiedUserID = unifiedID
	target.UnifiedUserID = unifiedID

	// 2. Rate limiting
	if !a.limiter.allow(unifiedID) {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 429,
			StatusIcon: "⏳",
			Title:      "请求过于频繁",
			Body:       "您的操作频率已超过限制（每分钟 30 次），请稍后再试。",
		})
		return
	}

	text := strings.TrimSpace(msg.Text)

	// 3. Check for pending confirmation reply
	if a.handleConfirmationReply(ctx, plugin, target, unifiedID, text) {
		return
	}

	// 4. Injection detection
	if containsInjection(text) {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 400,
			StatusIcon: "🚫",
			Title:      "输入包含不安全字符",
			Body:       "检测到输入中包含 shell 元字符（如 ;、|、&&、`、$() 等），已拒绝处理。请使用纯文本描述您的操作。",
		})
		return
	}

	// 5. NL Router intent parsing
	intent, err := a.router.Parse(ctx, unifiedID, text)
	if err != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 500,
			StatusIcon: "❌",
			Title:      "意图解析失败",
			Body:       fmt.Sprintf("无法解析您的请求: %s", err.Error()),
		})
		return
	}

	// 6. High-risk confirmation prompt
	if isHighRiskIntent(intent) {
		a.requestConfirmation(ctx, plugin, target, unifiedID, intent)
		return
	}

	// 7. Execute intent
	resp, err := a.executor.Execute(ctx, unifiedID, intent)
	if err != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 500,
			StatusIcon: "❌",
			Title:      "执行失败",
			Body:       fmt.Sprintf("操作执行出错: %s\n建议: 请检查参数后重试，或发送 /help 查看帮助。", err.Error()),
		})
		return
	}

	// 8. Format and deliver response
	a.sendResponse(ctx, plugin, target, resp)
}

// ---------------------------------------------------------------------------
// Confirmation flow helpers
// ---------------------------------------------------------------------------

// requestConfirmation stores a pending confirmation and sends a prompt.
func (a *Adapter) requestConfirmation(ctx context.Context, plugin IMPlugin, target UserTarget, userID string, intent *nlrouter.Intent) {
	a.confirmMu.Lock()
	a.confirmations[userID] = &PendingConfirmation{
		ID:        fmt.Sprintf("confirm_%s_%d", userID, time.Now().UnixNano()),
		UserID:    userID,
		Intent:    intent,
		ExpiresAt: time.Now().Add(60 * time.Second),
	}
	a.confirmMu.Unlock()

	desc := intent.Name
	if intent.Name == nlrouter.IntentKillSession {
		desc = "终止会话 (kill_session)"
	} else if intent.Name == nlrouter.IntentLaunchSession {
		desc = "启动含系统命令的会话 (launch_session)"
	}

	a.sendResponse(ctx, plugin, target, &GenericResponse{
		StatusCode: 200,
		StatusIcon: "⚠️",
		Title:      "高风险操作确认",
		Body:       fmt.Sprintf("您即将执行高风险操作: %s\n\n请在 60 秒内回复 \"确认\"、\"yes\" 或 \"1\" 以继续，否则操作将自动取消。", desc),
	})
}

// handleConfirmationReply checks if the user has a pending confirmation and
// processes the reply. Returns true if a confirmation was handled.
func (a *Adapter) handleConfirmationReply(ctx context.Context, plugin IMPlugin, target UserTarget, userID, text string) bool {
	a.confirmMu.Lock()
	pending, ok := a.confirmations[userID]
	if !ok {
		a.confirmMu.Unlock()
		return false
	}

	// Check expiry
	if time.Now().After(pending.ExpiresAt) {
		delete(a.confirmations, userID)
		a.confirmMu.Unlock()
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 408,
			StatusIcon: "⏰",
			Title:      "确认超时",
			Body:       "高风险操作确认已超时（60 秒），操作已取消。",
		})
		return true
	}

	if !isConfirmationReply(text) {
		delete(a.confirmations, userID)
		a.confirmMu.Unlock()
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "ℹ️",
			Title:      "操作已取消",
			Body:       "高风险操作已取消。",
		})
		return true
	}

	// Confirmed — execute the intent.
	intent := pending.Intent
	delete(a.confirmations, userID)
	a.confirmMu.Unlock()

	resp, err := a.executor.Execute(ctx, userID, intent)
	if err != nil {
		a.sendResponse(ctx, plugin, target, &GenericResponse{
			StatusCode: 500,
			StatusIcon: "❌",
			Title:      "执行失败",
			Body:       fmt.Sprintf("操作执行出错: %s", err.Error()),
		})
		return true
	}
	a.sendResponse(ctx, plugin, target, resp)
	return true
}

// ---------------------------------------------------------------------------
// Response formatting & delivery (capability-based format selection)
// ---------------------------------------------------------------------------

// sendResponse delivers a GenericResponse to the user via the appropriate
// plugin method, choosing the best format based on CapabilityDeclaration.
//
// Strategy:
//   - If plugin supports rich cards → SendCard with OutgoingMessage
//   - Otherwise → SendText with FallbackText
func (a *Adapter) sendResponse(ctx context.Context, plugin IMPlugin, target UserTarget, resp *GenericResponse) {
	caps := plugin.Capabilities()
	out := resp.ToOutgoingMessage()

	if caps.SupportsRichCard {
		if err := plugin.SendCard(ctx, target, out); err != nil {
			log.Printf("[IM Adapter] SendCard failed for %s, falling back to text: %v", plugin.Name(), err)
			// Fallback to text on card send failure.
			_ = plugin.SendText(ctx, target, out.FallbackText)
		}
		return
	}

	// No rich card support — send plain text.
	text := out.FallbackText
	if text == "" {
		text = resp.ToFallbackText()
	}

	// Truncate if platform has a max text length.
	if caps.MaxTextLength > 0 && len(text) > caps.MaxTextLength {
		text = truncateAtLine(text, caps.MaxTextLength)
	}

	_ = plugin.SendText(ctx, target, text)
}

// truncateAtLine truncates text to maxLen at a line boundary and appends "…".
func truncateAtLine(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	// Reserve space for the ellipsis suffix.
	cutoff := maxLen - len("…")
	if cutoff < 0 {
		cutoff = 0
	}
	// Find the last newline before cutoff.
	idx := strings.LastIndex(text[:cutoff], "\n")
	if idx < 0 {
		idx = cutoff
	}
	return text[:idx] + "\n…"
}
