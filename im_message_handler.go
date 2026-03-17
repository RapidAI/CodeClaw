package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// IMMessageHandler — handles IM messages forwarded from Hub via WebSocket
// ---------------------------------------------------------------------------

// IMUserMessage is the payload of an "im.user_message" from Hub.
type IMUserMessage struct {
	UserID   string `json:"user_id"`
	Platform string `json:"platform"`
	Text     string `json:"text"`
}

// IMAgentResponse is the structured reply sent back to Hub.
type IMAgentResponse struct {
	Text         string             `json:"text"`
	Fields       []IMResponseField  `json:"fields,omitempty"`
	Actions      []IMResponseAction `json:"actions,omitempty"`
	ImageKey     string             `json:"image_key,omitempty"`
	FileData     string             `json:"file_data,omitempty"`
	FileName     string             `json:"file_name,omitempty"`
	FileMimeType string             `json:"file_mime_type,omitempty"`
	Error        string             `json:"error,omitempty"`
}

// IMResponseField is a key-value field in the agent response.
type IMResponseField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// IMResponseAction is a suggested action in the agent response.
type IMResponseAction struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Style   string `json:"style"`
}

// ---------------------------------------------------------------------------
// Conversation Memory
// ---------------------------------------------------------------------------

const (
	maxConversationTurns   = 40
	maxMemoryTokenEstimate = 80_000
	memoryTTL              = 2 * time.Hour  // 对话记忆过期时间
	memoryCleanupInterval  = 10 * time.Minute
)

type conversationEntry struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  interface{} `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// toMessage converts a conversationEntry to a map suitable for the LLM API.
func (e conversationEntry) toMessage() interface{} {
	m := map[string]interface{}{"role": e.Role, "content": e.Content}
	if e.ToolCalls != nil {
		m["tool_calls"] = e.ToolCalls
	}
	if e.ToolCallID != "" {
		m["tool_call_id"] = e.ToolCallID
	}
	return m
}

type conversationSession struct {
	entries    []conversationEntry
	lastAccess time.Time
}

type conversationMemory struct {
	mu       sync.RWMutex
	sessions map[string]*conversationSession
	stopCh   chan struct{}
	archiver *ConversationArchiver
}

func newConversationMemory() *conversationMemory {
	cm := &conversationMemory{
		sessions: make(map[string]*conversationSession),
		stopCh:   make(chan struct{}),
	}
	go cm.evictionLoop()
	return cm
}

// evictionLoop 定期清理过期的对话记忆，防止内存无限增长
func (cm *conversationMemory) evictionLoop() {
	ticker := time.NewTicker(memoryCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cm.evictExpired()
		case <-cm.stopCh:
			return
		}
	}
}

func (cm *conversationMemory) evictExpired() {
	now := time.Now()
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for uid, s := range cm.sessions {
		if now.Sub(s.lastAccess) > memoryTTL {
			if cm.archiver != nil {
				if err := cm.archiver.Archive(uid, s.entries); err != nil {
					fmt.Fprintf(os.Stderr, "conversation_archiver: failed to archive user %s: %v\n", uid, err)
				}
			}
			delete(cm.sessions, uid)
		}
	}
}

func (cm *conversationMemory) stop() {
	select {
	case cm.stopCh <- struct{}{}:
	default:
	}
}

func (cm *conversationMemory) load(userID string) []conversationEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	s := cm.sessions[userID]
	if s == nil {
		return nil
	}
	out := make([]conversationEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (cm *conversationMemory) save(userID string, entries []conversationEntry) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.sessions[userID] = &conversationSession{
		entries:    entries,
		lastAccess: time.Now(),
	}
}

func (cm *conversationMemory) clear(userID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.sessions, userID)
}

func estimateTokens(entries []conversationEntry) int {
	total := 0
	for _, e := range entries {
		data, _ := json.Marshal(e)
		total += len(data)
	}
	return total / 4
}

func trimHistory(entries []conversationEntry) []conversationEntry {
	if len(entries) <= maxConversationTurns {
		return entries
	}
	return entries[len(entries)-maxConversationTurns:]
}

// ---------------------------------------------------------------------------
// IMMessageHandler
// ---------------------------------------------------------------------------

// toolsCacheTTL is the maximum age of the cached tool definitions.
// When MCP_Registry changes, tools are regenerated within this window.
const toolsCacheTTL = 5 * time.Second

// ProgressCallback is called by the agent loop to send intermediate progress
// updates to the user via IM while the agent is still working. This prevents
// timeout on long-running tasks (e.g. file search, large builds).
type ProgressCallback func(text string)

// IMMessageHandler processes IM messages using the local LLM Agent.
// It accesses mcpRegistry and skillExecutor via h.app at call time
// (not captured at construction) to handle late initialization.
type IMMessageHandler struct {
	app     *App
	manager *RemoteSessionManager
	memory  *conversationMemory
	client  *http.Client

	// Dynamic tool generation and routing (lazily initialized via setters).
	toolDefGen     *ToolDefinitionGenerator
	toolRouter     *ToolRouter
	cachedTools    []map[string]interface{}
	toolsCacheTime time.Time
	toolsMu        sync.RWMutex

	// Capability gap detection (lazily initialized via setter).
	capabilityGapDetector *CapabilityGapDetector

	// Long-term memory store (lazily initialized via setter).
	memoryStore *MemoryStore

	// Session template manager (lazily initialized via setter).
	templateManager *SessionTemplateManager

	// Smart session startup components (lazily initialized via setters).
	contextResolver *SessionContextResolver
	sessionPrecheck *SessionPrecheck
	startupFeedback *SessionStartupFeedback

	// Configuration manager (lazily initialized via setter).
	configManager *ConfigManager

	// Dynamic loop limit — set by the "set_max_iterations" tool during an
	// active agent loop. Reset to 0 at the start of each runAgentLoop call.
	// A positive value overrides the configured maxIter for the current loop.
	loopMaxOverride int
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	return &IMMessageHandler{
		app:     app,
		manager: manager,
		memory:  newConversationMemory(),
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// SetToolDefGenerator configures the dynamic tool definition generator.
// When set, it replaces the hardcoded buildToolDefinitions() output.
func (h *IMMessageHandler) SetToolDefGenerator(gen *ToolDefinitionGenerator) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolDefGen = gen
	// Invalidate cache so next call regenerates.
	h.cachedTools = nil
	h.toolsCacheTime = time.Time{}
}

// SetCapabilityGapDetector configures the capability gap detector.
func (h *IMMessageHandler) SetCapabilityGapDetector(detector *CapabilityGapDetector) {
	h.capabilityGapDetector = detector
}

// SetToolRouter configures the tool router for context-aware tool filtering.
func (h *IMMessageHandler) SetToolRouter(router *ToolRouter) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolRouter = router
}

// SetContextResolver configures the session context resolver for auto-detecting
// project paths and recommending tools.
func (h *IMMessageHandler) SetContextResolver(resolver *SessionContextResolver) {
	h.contextResolver = resolver
}

// SetSessionPrecheck configures the session precheck for environment validation.
func (h *IMMessageHandler) SetSessionPrecheck(precheck *SessionPrecheck) {
	h.sessionPrecheck = precheck
}

// SetStartupFeedback configures the startup feedback monitor.
func (h *IMMessageHandler) SetStartupFeedback(feedback *SessionStartupFeedback) {
	h.startupFeedback = feedback
}

// SetConfigManager configures the configuration manager for config tools.
func (h *IMMessageHandler) SetConfigManager(cm *ConfigManager) {
	h.configManager = cm
}

// SetMemoryStore configures the long-term memory store.
func (h *IMMessageHandler) SetMemoryStore(ms *MemoryStore) {
	h.memoryStore = ms
}

// SetTemplateManager configures the session template manager.
func (h *IMMessageHandler) SetTemplateManager(tm *SessionTemplateManager) {
	h.templateManager = tm
}

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
	h.toolsMu.RLock()
	gen := h.toolDefGen
	cached := h.cachedTools
	cacheTime := h.toolsCacheTime
	h.toolsMu.RUnlock()

	// Fallback: no generator configured — use hardcoded definitions.
	if gen == nil {
		return h.buildToolDefinitions()
	}

	// Return cached tools if still fresh (within 5 seconds).
	if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
		return cached
	}

	// Regenerate from the generator.
	tools := gen.Generate()

	h.toolsMu.Lock()
	h.cachedTools = tools
	h.toolsCacheTime = time.Now()
	h.toolsMu.Unlock()

	return tools
}

// routeTools applies the ToolRouter to filter tools based on user message.
// If no router is configured, returns allTools unchanged.
func (h *IMMessageHandler) routeTools(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	h.toolsMu.RLock()
	router := h.toolRouter
	h.toolsMu.RUnlock()

	if router == nil {
		return allTools
	}
	return router.Route(userMessage, allTools)
}

// HandleIMMessage processes an IM user message and returns the Agent's response.
func (h *IMMessageHandler) HandleIMMessage(msg IMUserMessage) *IMAgentResponse {
	return h.HandleIMMessageWithProgress(msg, nil)
}

// HandleIMMessageWithProgress processes an IM message with an optional progress
// callback. When onProgress is non-nil, the agent loop sends intermediate status
// updates (e.g. "正在执行 bash 命令…") so the Hub can relay them to the user
// and reset the response timeout — preventing 504 on long-running tasks.
func (h *IMMessageHandler) HandleIMMessageWithProgress(msg IMUserMessage, onProgress ProgressCallback) *IMAgentResponse {
	trimmed := strings.TrimSpace(msg.Text)

	// Slash commands are processed before the LLM config check — they don't
	// need LLM and must always work so users can manage state even when LLM
	// is misconfigured.
	if trimmed == "/new" || trimmed == "/reset" || trimmed == "/clear" {
		h.memory.clear(msg.UserID)
		return &IMAgentResponse{Text: "对话已重置。"}
	}
	if trimmed == "/exit" || trimmed == "/quit" {
		return h.handleExitCommand(msg.UserID)
	}
	if trimmed == "/sessions" || trimmed == "/status" {
		return h.handleSessionsCommand()
	}
	if trimmed == "/help" {
		return &IMAgentResponse{Text: "📖 可用命令:\n" +
			"/new /reset — 重置对话\n" +
			"/exit /quit — 终止所有会话，退出编程模式\n" +
			"/sessions — 查看当前会话状态\n" +
			"/help — 显示此帮助"}
	}

	if !h.app.isMaclawLLMConfigured() {
		return &IMAgentResponse{
			Error: "MaClaw LLM 未配置，无法处理请求。请在 MaClaw 客户端的设置中配置 LLM。",
		}
	}

	history := h.memory.load(msg.UserID)
	history = h.compactHistory(history)
	var systemPrompt string
	if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(msg.Text)
	} else {
		systemPrompt = h.buildSystemPrompt()
	}
	return h.runAgentLoop(msg.UserID, systemPrompt, history, msg.Text, onProgress)
}

// handleExitCommand terminates all active sessions, resets conversation
// memory, and returns the user to normal chat mode.
func (h *IMMessageHandler) handleExitCommand(userID string) *IMAgentResponse {
	var killed []string
	var failCount int
	if h.manager != nil {
		for _, s := range h.manager.List() {
			s.mu.RLock()
			active := isActiveRemoteSessionStatus(s.Status)
			sid := s.ID
			tool := s.Tool
			s.mu.RUnlock()
			if active {
				if err := h.manager.Kill(sid); err == nil {
					killed = append(killed, fmt.Sprintf("%s(%s)", sid, tool))
				} else {
					failCount++
				}
			}
		}
	}
	h.memory.clear(userID)

	var b strings.Builder
	if len(killed) > 0 {
		b.WriteString(fmt.Sprintf("已退出编程模式。终止了 %d 个会话: %s", len(killed), strings.Join(killed, ", ")))
	} else {
		b.WriteString("已退出编程模式。")
	}
	if failCount > 0 {
		b.WriteString(fmt.Sprintf("\n⚠️ %d 个会话终止失败，可能需要手动处理。", failCount))
	}
	b.WriteString("\n对话已重置，后续消息将正常对话。")
	return &IMAgentResponse{Text: b.String()}
}

// handleSessionsCommand returns a quick status summary of active sessions.
func (h *IMMessageHandler) handleSessionsCommand() *IMAgentResponse {
	if h.manager == nil {
		return &IMAgentResponse{Text: "会话管理器未初始化。"}
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return &IMAgentResponse{
			Text: "当前没有活跃会话。\n\n💡 提示: 发送 /exit 可退出编程模式回到普通对话。",
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 当前 %d 个会话:\n", len(sessions)))
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("• [%s] %s — %s", s.ID, s.Tool, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" | %s", task))
		}
		if waiting {
			b.WriteString(" ⏳等待输入")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n💡 发送 /exit 可终止所有会话并退出编程模式。")
	return &IMAgentResponse{Text: b.String()}
}

// compactHistory summarizes old conversation turns to stay within token limits.
func (h *IMMessageHandler) compactHistory(entries []conversationEntry) []conversationEntry {
	if estimateTokens(entries) < maxMemoryTokenEstimate {
		return entries
	}
	split := len(entries) / 2
	recent := entries[split:]

	var sb strings.Builder
	for _, e := range entries[:split] {
		data, _ := json.Marshal(e)
		sb.Write(data)
		sb.WriteByte('\n')
	}
	summaryText := sb.String()
	if len(summaryText) > 32000 {
		summaryText = summaryText[:32000] + "\n...(truncated)"
	}

	cfg := h.app.GetMaclawLLMConfig()
	msgs := []map[string]string{
		{"role": "user", "content": "请简洁总结以下对话历史，保留关键事实、决策和待办事项：\n\n" + summaryText},
	}
	conv := make([]interface{}, len(msgs))
	for i, m := range msgs {
		conv[i] = m
	}
	resp, err := h.doLLMRequest(cfg, conv, nil)
	if err != nil || len(resp.Choices) == 0 {
		return recent
	}

	compacted := []conversationEntry{
		{Role: "user", Content: "[对话历史摘要]\n" + resp.Choices[0].Message.Content},
		{Role: "assistant", Content: "好的，我已了解之前的对话上下文。"},
	}
	return append(compacted, recent...)
}

// ---------------------------------------------------------------------------
// LLM types and HTTP client
// ---------------------------------------------------------------------------

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}

type llmChoice struct {
	Message      llmMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type llmMessage struct {
	Role      string        `json:"role"`
	Content   string        `json:"content"`
	ToolCalls []llmToolCall `json:"tool_calls,omitempty"`
}

type llmToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// doLLMRequest sends a chat completion request to the configured LLM.
func (h *IMMessageHandler) doLLMRequest(cfg MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"

	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	var result llmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Agentic Loop — multi-round tool calling
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) runAgentLoop(userID, systemPrompt string, history []conversationEntry, userText string, onProgress ProgressCallback) (result *IMAgentResponse) {
	// panic recovery — 防止工具执行异常导致 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent 内部错误: %v", r)}
		}
	}()

	// Helper to send progress if callback is set.
	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	cfg := h.app.GetMaclawLLMConfig()
	maxIter := h.app.GetMaclawAgentMaxIterations()
	h.loopMaxOverride = 0 // reset dynamic override for this loop
	allTools := h.getTools()
	tools := h.routeTools(userText, allTools)

	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.toMessage())
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": userText})

	history = append(history, conversationEntry{Role: "user", Content: userText})

	// maxIter == 0 means "unlimited" — agent decides when to stop.
	// We still enforce a hard safety cap of 200 to prevent runaway loops.
	effectiveMax := maxIter
	if effectiveMax <= 0 {
		effectiveMax = 200
	}

	for iteration := 0; ; iteration++ {
		// Check dynamic override from set_max_iterations tool each iteration.
		if h.loopMaxOverride > 0 {
			effectiveMax = h.loopMaxOverride
		}
		if iteration >= effectiveMax {
			break
		}
		if iteration > 0 {
			if maxIter > 0 || h.loopMaxOverride > 0 {
				sendProgress(fmt.Sprintf("🔄 Agent 推理中（第 %d/%d 轮）…", iteration+1, effectiveMax))
			} else {
				sendProgress(fmt.Sprintf("🔄 Agent 推理中（第 %d 轮）…", iteration+1))
			}
		}
		resp, err := h.doLLMRequest(cfg, conversation, tools)
		if err != nil {
			return &IMAgentResponse{Error: fmt.Sprintf("LLM 调用失败: %s", err.Error())}
		}
		if len(resp.Choices) == 0 {
			return &IMAgentResponse{Error: "LLM 未返回有效回复"}
		}

		choice := resp.Choices[0]

		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": choice.Message.Content,
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		historyEntry := conversationEntry{Role: "assistant", Content: choice.Message.Content}
		if len(choice.Message.ToolCalls) > 0 {
			historyEntry.ToolCalls = choice.Message.ToolCalls
		}
		history = append(history, historyEntry)

		// No tool calls → final response.
		// NOTE: Some LLM providers (e.g. DeepSeek, Qwen) return finish_reason="stop"
		// even when tool_calls are present. We must check tool_calls first and only
		// treat the response as final when there are genuinely no tool calls.
		if len(choice.Message.ToolCalls) == 0 {
			// Check for capability gap before returning.
			if h.capabilityGapDetector != nil && h.capabilityGapDetector.Detect(choice.Message.Content) {
				skillName, result, err := h.capabilityGapDetector.Resolve(
					context.Background(), userText, nil,
					func(status string) {
						// Status updates are logged but not sent to user in this context.
					},
				)
				if skillName != "" && err == nil {
					finalText := fmt.Sprintf("✅ 已自动安装并执行 Skill「%s」\n%s", skillName, result)
					h.memory.save(userID, trimHistory(history))
					return &IMAgentResponse{Text: finalText}
				}
			}
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{Text: choice.Message.Content}
		}

		// Execute tool calls and feed results back.
		var pendingImageKey string
		var pendingFileData, pendingFileName, pendingFileMimeType string
		for _, tc := range choice.Message.ToolCalls {
			sendProgress(fmt.Sprintf("⚙️ 正在执行工具: %s", tc.Function.Name))
			result := h.executeTool(tc.Function.Name, tc.Function.Arguments, onProgress)

			// Intercept direct screenshot results: extract base64 image data
			// so it can be delivered via IM image channel instead of text.
			toolContent := result
			if strings.HasPrefix(result, "[screenshot_base64]") {
				pendingImageKey = strings.TrimPrefix(result, "[screenshot_base64]")
				toolContent = "截图已成功捕获，将作为图片发送给用户。"
			}

			// Intercept file send results: extract base64 file data
			// Format: [file_base64|filename|mimetype]data
			if strings.HasPrefix(result, "[file_base64|") {
				rest := strings.TrimPrefix(result, "[file_base64|")
				if closeBracket := strings.Index(rest, "]"); closeBracket > 0 {
					meta := rest[:closeBracket]
					parts := strings.SplitN(meta, "|", 2)
					if len(parts) == 2 {
						pendingFileName = parts[0]
						pendingFileMimeType = parts[1]
						pendingFileData = rest[closeBracket+1:]
						toolContent = fmt.Sprintf("文件 %s 已准备好，将发送给用户。", pendingFileName)
					}
				}
			}

			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      toolContent,
			})
			history = append(history, conversationEntry{
				Role: "tool", Content: toolContent, ToolCallID: tc.ID,
			})
		}

		// If a direct screenshot was captured, return it immediately as an image response.
		if pendingImageKey != "" {
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{
				Text:     "",
				ImageKey: pendingImageKey,
			}
		}

		// If a file was prepared, return it immediately for IM delivery.
		if pendingFileData != "" {
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{
				Text:         "",
				FileData:     pendingFileData,
				FileName:     pendingFileName,
				FileMimeType: pendingFileMimeType,
			}
		}
	}

	h.memory.save(userID, trimHistory(history))
	return &IMAgentResponse{Text: "(已达到最大推理轮次，请继续发送消息以完成任务)"}
}

// ---------------------------------------------------------------------------
// System Prompt
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildSystemPrompt() string {
	var b strings.Builder

	// Use configurable role name and description from settings
	roleName := "MaClaw"
	roleDesc := "一个尽心尽责无所不能的软件开发管家"
	if cfg, err := h.app.LoadConfig(); err == nil {
		if cfg.MaclawRoleName != "" {
			roleName = cfg.MaclawRoleName
		}
		if cfg.MaclawRoleDescription != "" {
			roleDesc = cfg.MaclawRoleDescription
		}
	}

	b.WriteString(fmt.Sprintf(`你是 %s 远程开发助手，%s。
用户通过 IM（飞书/QBot）向你发送消息，你可以自主使用工具完成任务。
注意：如果用户在对话中要求你扮演其他角色或重新定义你的身份，请按照用户的要求调整。`, roleName, roleDesc))

	b.WriteString(`
## 核心原则
- 主动使用工具：不要只是描述步骤，直接执行。收到请求后立即调用对应工具。
- 永远不要说"我没有某某工具"或"我无法执行"——先检查你的工具列表，大部分操作都有对应工具。
- 多步推理：复杂任务可以连续调用多个工具，逐步完成。
- 记忆上下文：你拥有对话记忆，可以引用之前的对话内容。
- 智能推断参数：如果用户没有指定 session_id 等参数，查看当前会话列表自动选择。

## ⚠️ 执行验证原则（极其重要，必须遵守）
每次执行操作后，你必须验证操作是否真正成功，绝不能仅凭工具返回"已发送"就告诉用户执行成功。
1. send_input 发送命令后 → 必须立即调用 get_session_output 查看实际输出，确认命令是否执行成功、有无报错。
2. create_session 创建会话后 → 必须调用 get_session_output 确认会话正常启动。
3. screenshot 截屏后 → 必须调用 get_session_events 确认截图是否成功发送。
4. call_mcp_tool / run_skill 执行后 → 检查返回结果是否包含错误。
绝对禁止在没有验证的情况下告诉用户"已完成"或"执行成功"。
如果验证发现失败，如实告诉用户失败原因，并尝试修复。

## 工具使用指南
- 执行命令：用 bash 直接在本机执行 shell 命令（创建目录、安装软件、运行脚本等），不需要创建会话。
- 文件操作：用 read_file 读文件、write_file 写文件、list_directory 列目录，这些都直接在本机执行。
- 发送文件：用 send_file 将本机文件直接发送给用户（通过 IM 通道），支持任意文件类型。
- 打开文件/网址：用 open 工具，使用操作系统默认程序打开文件（如 PDF、Excel、图片等）或用默认浏览器打开网址。
- 截屏：直接调用 screenshot 工具。无需活跃会话也能截取本机桌面，有会话时会自动选择。
- 创建会话：用 create_session，创建后必须用 get_session_output 确认启动。
- 发送命令：用 send_input，发送后必须用 get_session_output 确认结果。
- 查看输出：用 get_session_output 获取会话最近输出。
- 并行任务：用 parallel_execute 同时执行多个任务。
- MCP 工具：用 list_mcp_tools 查看可用工具，用 call_mcp_tool 调用。
- Skill：用 list_skills 查看本地已注册的 Skill，用 run_skill 执行。如果本地没有合适的 Skill，用 search_skill_hub 在 SkillHub 上搜索，找到后用 install_skill_hub 安装并自动执行（默认 auto_run=true，安装后立即运行）。

注意：简单的文件操作和命令执行请直接用 bash/read_file/write_file/list_directory，不要绕道创建会话。

## 会话恢复
- 当用户发送"继续"、"恢复"、"resume"等意图时，调用 list_sessions 列出可恢复的会话（状态为 running 或 paused）
- 展示最近 5 个可恢复会话的 ID、工具、项目和状态
- 用户选择后，调用 get_session_output 获取最近输出摘要展示给用户
- 恢复后自动进入该会话的交互模式，后续用户消息通过 send_input 转发
- 如果会话已终止，提示用户并建议使用相同配置创建新会话

## 自然语言启动
当用户用自然语言描述编程任务时（如"帮我用 Claude 修复 myproject 的 bug"），你应该：
1. 从消息中提取：工具名称（如 Claude/Codex/Gemini）、项目标识（项目名或路径）、任务描述
2. 如果无法确定工具名称，使用 recommend_tool 工具推荐
3. 如果无法确定项目路径，create_session 会自动推断（需求 1 自动项目检测）
4. 在创建会话前，向用户确认解析出的参数（工具、项目、任务）
5. 用户确认后，调用 create_session 创建会话，并将任务描述通过 send_input 发送到会话

`)
	b.WriteString("## 当前设备状态\n")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "MaClaw Desktop"
	}
	b.WriteString(fmt.Sprintf("- 设备名: %s\n", hostname))
	b.WriteString(fmt.Sprintf("- 平台: %s\n", normalizedRemotePlatform()))
	b.WriteString(fmt.Sprintf("- App 版本: %s\n", remoteAppVersion()))

	if h.manager != nil {
		sessions := h.manager.List()
		b.WriteString(fmt.Sprintf("- 活跃会话: %d 个\n", len(sessions)))
		if len(sessions) > 0 {
			b.WriteString("\n## 当前会话列表\n")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				task := s.Summary.CurrentTask
				lastResult := s.Summary.LastResult
				s.mu.RUnlock()
				b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
				if task != "" {
					b.WriteString(fmt.Sprintf(" 当前任务=%s", task))
				}
				if lastResult != "" {
					b.WriteString(fmt.Sprintf(" 最近结果=%s", lastResult))
				}
				b.WriteString("\n")
			}
		}
	}

	if h.app.mcpRegistry != nil {
		servers := h.app.mcpRegistry.ListServers()
		if len(servers) > 0 {
			b.WriteString("\n## 已注册 MCP Server\n")
			for _, s := range servers {
				b.WriteString(fmt.Sprintf("- [%s] %s 状态=%s\n", s.ID, s.Name, s.HealthStatus))
			}
		}
	}

	if h.app.skillExecutor != nil {
		skills := h.app.skillExecutor.List()
		if len(skills) > 0 {
			b.WriteString("\n## 已注册 Skill\n")
			for _, s := range skills {
				if s.Status == "active" {
					b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
				}
			}
		}
	}

	b.WriteString("\n## 配置管理\n")
	b.WriteString("- 用户可以通过自然语言修改配置，如\"把默认工具改成 Gemini\"、\"关闭省电模式\"\n")
	b.WriteString("- 使用 list_config_schema 了解所有可配置项\n")
	b.WriteString("- 修改前使用 get_config 查看当前值，修改后确认变更\n")
	b.WriteString("- 批量修改使用 batch_update_config，确保原子性\n")
	b.WriteString("- 导出配置使用 export_config，导入使用 import_config\n")
	b.WriteString("- 敏感信息（API Key、Token）在展示时会自动脱敏\n")

	b.WriteString("\n## 自管理能力\n")
	b.WriteString("- 你可以使用 set_max_iterations 工具动态调整当前对话的最大推理轮数。\n")
	b.WriteString("- 当你判断任务复杂、需要多步操作时，主动调用 set_max_iterations 扩展轮数上限。\n")
	b.WriteString("- 当任务简单、即将完成时，无需调整。\n")
	b.WriteString("- 此调整仅影响当前对话，不会修改全局配置。\n")

	b.WriteString("\n## 对话管理\n")
	b.WriteString("- 用户发送 /new 或 /reset 可重置对话\n")
	b.WriteString("- 用户发送 /exit 或 /quit 可终止所有编程会话并退出编程模式，回到普通对话\n")
	b.WriteString("- 用户发送 /sessions 可快速查看当前会话状态\n")
	b.WriteString("- 用户发送 /help 可查看所有可用命令\n")
	b.WriteString("- 当用户表达想退出、结束、不做了、回到普通聊天等意图时，提醒用户发送 /exit\n")
	b.WriteString("- 你拥有多轮对话记忆，可以引用之前的上下文\n")
	b.WriteString("\n请用中文回复，关键技术术语保留英文。回复要简洁实用。")

	// Inject long-term memory section if memoryStore is available.
	h.appendMemorySection(&b, "")

	return b.String()
}

// buildSystemPromptWithMemory builds the system prompt with memory recall
// tailored to the user's current message for better relevance.
func (h *IMMessageHandler) buildSystemPromptWithMemory(userMessage string) string {
	var b strings.Builder
	base := h.buildSystemPrompt()
	b.WriteString(base)

	// If buildSystemPrompt already appended a generic memory section (with
	// empty query), we don't double-append. Instead, we replace it by
	// rebuilding without the generic section. However, since
	// buildSystemPrompt always calls appendMemorySection(""), we need to
	// strip that and re-append with the real user message.
	//
	// Simpler approach: buildSystemPrompt already appended with empty query.
	// If userMessage is non-empty, we do a targeted recall and append an
	// additional "## 相关记忆补充" section with any extra entries the
	// targeted recall found that the generic one missed.
	//
	// Actually, the cleanest approach: just return the base prompt as-is
	// when userMessage is empty, and when non-empty, strip the generic
	// memory section and re-append with the targeted query.

	// For simplicity and correctness: the base prompt already has the
	// generic memory section. When we have a real user message, we rebuild
	// the memory section with better relevance.
	if userMessage != "" && h.memoryStore != nil {
		// Find and strip the existing memory section appended by buildSystemPrompt.
		result := b.String()
		if idx := strings.Index(result, "\n## 用户记忆\n"); idx >= 0 {
			result = result[:idx]
		}
		// Re-build with targeted recall.
		var b2 strings.Builder
		b2.WriteString(result)
		h.appendMemorySection(&b2, userMessage)
		return b2.String()
	}

	return b.String()
}

// appendMemorySection appends the "## 用户记忆" section to the builder using
// recalled memories. Pass an empty userMessage for a generic recall, or the
// actual user message for relevance-ranked recall.
func (h *IMMessageHandler) appendMemorySection(b *strings.Builder, userMessage string) {
	if h.memoryStore == nil {
		return
	}

	memories := h.memoryStore.Recall(userMessage)
	if len(memories) == 0 {
		return
	}

	b.WriteString("\n## 用户记忆\n")
	b.WriteString("以下是关于用户的长期记忆，请在回复中参考这些信息：\n")
	for _, m := range memories {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", string(m.Category), m.Content))
	}

	// Touch access counts for recalled memories.
	ids := make([]string, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
	}
	h.memoryStore.TouchAccess(ids)

	b.WriteString("\n## 记忆管理指引\n")
	b.WriteString("当你在对话中识别到以下信息时，请主动调用 save_memory 工具保存：\n")
	b.WriteString("- 用户的个人信息（姓名、称呼、角色等）→ category: user_fact\n")
	b.WriteString("- 用户的偏好（喜欢的工具、编码风格、语言偏好等）→ category: preference\n")
	b.WriteString("- 项目相关知识（架构决策、技术栈、约定等）→ category: project_knowledge\n")
	b.WriteString("- 用户的指令或规则（\"以后都用XX\"、\"不要做YY\"等）→ category: instruction\n")
	b.WriteString("无需每次都询问用户是否保存，识别到有价值的信息时直接保存即可。\n")
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("list_sessions", "列出当前所有远程会话及其状态", nil, nil),
		toolDef("create_session", "创建新的远程会话。可指定 provider 选择服务商。创建后建议用 get_session_output 观察启动状态。",
			map[string]interface{}{
				"tool":         map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
				"project_path": map[string]string{"type": "string", "description": "项目路径（可选）"},
				"provider":     map[string]string{"type": "string", "description": "服务商名称（可选，如 Original, DeepSeek, 百度千帆）。不指定则使用桌面端当前选中的服务商"},
			}, []string{"tool"}),
		toolDef("list_providers", "列出指定编程工具的所有可用服务商（已过滤未配置的空服务商）",
			map[string]interface{}{
				"tool": map[string]string{"type": "string", "description": "工具名称，如 claude, codex, gemini"},
			}, []string{"tool"}),
		toolDef("send_input", "向指定会话发送文本输入。发送后可用 get_session_output 观察结果。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
				"text":       map[string]string{"type": "string", "description": "要发送的文本"},
			}, []string{"session_id", "text"}),
		toolDef("get_session_output", "获取指定会话的最近输出内容和状态摘要。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
				"lines":      map[string]string{"type": "integer", "description": "返回最近 N 行输出（默认 30，最大 100）"},
			}, []string{"session_id"}),
		toolDef("get_session_events", "获取指定会话的重要事件列表（文件修改、命令执行、错误等）",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("interrupt_session", "中断指定会话（发送 Ctrl+C 信号）",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("kill_session", "终止指定会话",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("screenshot", "截取屏幕截图并发送给用户。如果有活跃会话可指定 session_id，没有活跃会话时会直接截取本机桌面屏幕（不需要创建会话）。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID（可选，只有一个会话时自动选择）"},
			}, nil),
		toolDef("list_mcp_tools", "列出已注册的 MCP Server 及其工具", nil, nil),
		toolDef("call_mcp_tool", "调用指定 MCP Server 上的工具",
			map[string]interface{}{
				"server_id": map[string]string{"type": "string", "description": "MCP Server ID"},
				"tool_name": map[string]string{"type": "string", "description": "工具名称"},
				"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
			}, []string{"server_id", "tool_name"}),
		toolDef("list_skills", "列出已注册的本地 Skill。如果本地没有 Skill，会同时展示 SkillHub 上的推荐 Skill 供安装。", nil, nil),
		toolDef("search_skill_hub", "在已配置的 SkillHub（如 openclaw、tencent 等）上搜索可用的 Skill",
			map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词（如 'git commit'、'代码审查'、'部署'）"},
			}, []string{"query"}),
		toolDef("install_skill_hub", "从 SkillHub 安装指定的 Skill 到本地。设置 auto_run=true 可安装后立即执行。",
			map[string]interface{}{
				"skill_id": map[string]string{"type": "string", "description": "Skill ID（从 search_skill_hub 结果中获取）"},
				"hub_url":  map[string]string{"type": "string", "description": "来源 Hub URL（从 search_skill_hub 结果中获取）"},
				"auto_run": map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（默认 true）"},
			}, []string{"skill_id", "hub_url"}),
		toolDef("run_skill", "执行指定的 Skill",
			map[string]interface{}{
				"name": map[string]string{"type": "string", "description": "Skill 名称"},
			}, []string{"name"}),
		toolDef("parallel_execute", "并行执行多个编程任务，每个任务在独立会话中运行（最多5个）",
			map[string]interface{}{
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "任务列表，每个任务包含 tool（工具名）、description（任务描述）、project_path（项目路径）",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"tool":         map[string]string{"type": "string", "description": "工具名称"},
							"description":  map[string]string{"type": "string", "description": "任务描述"},
							"project_path": map[string]string{"type": "string", "description": "项目路径"},
						},
					},
				},
			}, []string{"tasks"}),
		toolDef("recommend_tool", "根据任务描述推荐最合适的编程工具",
			map[string]interface{}{
				"task_description": map[string]string{"type": "string", "description": "任务描述"},
			}, []string{"task_description"}),
		// --- 本机直接操作工具 ---
		toolDef("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
			map[string]interface{}{
				"command":     map[string]string{"type": "string", "description": "要执行的 shell 命令"},
				"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为用户主目录）"},
				"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
			}, []string{"command"}),
		toolDef("read_file", "读取本机文件内容",
			map[string]interface{}{
				"path":  map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
				"lines": map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200）"},
			}, []string{"path"}),
		toolDef("write_file", "写入内容到本机文件（会创建不存在的目录）",
			map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "文件路径"},
				"content": map[string]string{"type": "string", "description": "文件内容"},
			}, []string{"path", "content"}),
		toolDef("list_directory", "列出本机目录内容",
			map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "目录路径（可选，默认为用户主目录）"},
			}, nil),
		toolDef("send_file", "读取本机文件并发送给用户（通过 IM 通道直接发送文件）",
			map[string]interface{}{
				"path":      map[string]string{"type": "string", "description": "文件的绝对路径或相对于主目录的路径"},
				"file_name": map[string]string{"type": "string", "description": "发送时显示的文件名（可选，默认使用原文件名）"},
			}, []string{"path"}),
		toolDef("open", "用操作系统默认程序打开文件或网址。例如：打开 PDF 用默认阅读器、打开 .xlsx 用 Excel、打开 URL 用默认浏览器、打开文件夹用资源管理器。也支持 mailto: 链接。",
			map[string]interface{}{
				"target": map[string]string{"type": "string", "description": "要打开的文件路径、目录路径或 URL（如 C:\\Users\\test\\doc.pdf、https://example.com、mailto:test@example.com）"},
			}, []string{"target"}),
		// --- 长期记忆工具 ---
		toolDef("save_memory", "保存一条记忆到长期记忆存储",
			map[string]interface{}{
				"content":  map[string]string{"type": "string", "description": "记忆内容"},
				"category": map[string]string{"type": "string", "description": "类别: user_fact/preference/project_knowledge/instruction"},
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "关联标签",
					"items":       map[string]string{"type": "string"},
				},
			}, []string{"content", "category"}),
		toolDef("list_memories", "列出或搜索长期记忆",
			map[string]interface{}{
				"category": map[string]string{"type": "string", "description": "按类别过滤"},
				"keyword":  map[string]string{"type": "string", "description": "按关键词搜索"},
			}, nil),
		toolDef("delete_memory", "删除一条长期记忆",
			map[string]interface{}{
				"id": map[string]string{"type": "string", "description": "记忆条目 ID"},
			}, []string{"id"}),
		// --- 会话模板工具 ---
		toolDef("create_template", "创建会话模板（快捷启动配置）",
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "模板名称"},
				"tool":         map[string]string{"type": "string", "description": "工具名称"},
				"project_path": map[string]string{"type": "string", "description": "项目路径"},
				"model_config": map[string]string{"type": "string", "description": "模型配置"},
				"yolo_mode":    map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式"},
			}, []string{"name", "tool"}),
		toolDef("list_templates", "列出所有会话模板", nil, nil),
		toolDef("launch_template", "使用模板启动会话",
			map[string]interface{}{
				"template_name": map[string]string{"type": "string", "description": "模板名称"},
			}, []string{"template_name"}),
		// --- 配置管理工具 ---
		toolDef("get_config", "获取指定配置区域的当前值",
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "配置区域名称（如 claude/gemini/remote/projects/maclaw_llm/proxy/general），为空或 all 返回概览"},
			}, []string{"section"}),
		toolDef("update_config", "修改单个配置项",
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "配置区域名称"},
				"key":     map[string]string{"type": "string", "description": "配置项名称"},
				"value":   map[string]string{"type": "string", "description": "新值"},
			}, []string{"section", "key", "value"}),
		toolDef("batch_update_config", "批量修改配置（原子性，任一项失败则全部回滚）",
			map[string]interface{}{
				"changes": map[string]string{"type": "string", "description": "JSON 数组，每项包含 section/key/value，例如 [{\"section\":\"general\",\"key\":\"language\",\"value\":\"en\"}]"},
			}, []string{"changes"}),
		toolDef("list_config_schema", "列出所有可配置项的 schema 信息", nil, nil),
		toolDef("export_config", "导出当前配置（敏感字段已脱敏）", nil, nil),
		toolDef("import_config", "导入配置（JSON 格式，保留本机特有字段）",
			map[string]interface{}{
				"json_data": map[string]string{"type": "string", "description": "要导入的配置 JSON 字符串"},
			}, []string{"json_data"}),
		// --- Agent 自管理工具 ---
		toolDef("set_max_iterations", "动态调整当前对话的最大推理轮数。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。仅影响当前对话，不修改全局配置。上限不超过 200。",
			map[string]interface{}{
				"max_iterations": map[string]string{"type": "integer", "description": "新的最大轮数（1-200）"},
				"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
			}, []string{"max_iterations"}),
	}
}

func toolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{"type": "object"}
	if props != nil {
		params["properties"] = props
	} else {
		params["properties"] = map[string]interface{}{}
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}

// ---------------------------------------------------------------------------
// Tool Execution
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress ProgressCallback) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行异常: %v", r)
		}
	}()

	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("参数解析失败: %s", err.Error())
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	switch name {
	case "list_sessions":
		return h.toolListSessions()
	case "create_session":
		return h.toolCreateSession(args)
	case "list_providers":
		return h.toolListProviders(args)
	case "send_input":
		return h.toolSendInput(args)
	case "get_session_output":
		return h.toolGetSessionOutput(args)
	case "get_session_events":
		return h.toolGetSessionEvents(args)
	case "interrupt_session":
		return h.toolInterruptSession(args)
	case "kill_session":
		return h.toolKillSession(args)
	case "screenshot":
		return h.toolScreenshot(args)
	case "list_mcp_tools":
		return h.toolListMCPTools()
	case "call_mcp_tool":
		return h.toolCallMCPTool(args)
	case "list_skills":
		return h.toolListSkills()
	case "search_skill_hub":
		return h.toolSearchSkillHub(args)
	case "install_skill_hub":
		return h.toolInstallSkillHub(args)
	case "run_skill":
		return h.toolRunSkill(args)
	case "parallel_execute":
		return h.toolParallelExecute(args)
	case "recommend_tool":
		return h.toolRecommendTool(args)
	case "bash":
		return h.toolBash(args, onProgress)
	case "read_file":
		return h.toolReadFile(args)
	case "write_file":
		return h.toolWriteFile(args)
	case "list_directory":
		return h.toolListDirectory(args)
	case "send_file":
		return h.toolSendFile(args)
	case "open":
		return h.toolOpen(args)
	case "save_memory":
		return h.toolSaveMemory(args)
	case "list_memories":
		return h.toolListMemories(args)
	case "delete_memory":
		return h.toolDeleteMemory(args)
	case "create_template":
		return h.toolCreateTemplate(args)
	case "list_templates":
		return h.toolListTemplates()
	case "launch_template":
		return h.toolLaunchTemplate(args)
	case "get_config":
		return h.toolGetConfig(args)
	case "update_config":
		return h.toolUpdateConfig(args)
	case "batch_update_config":
		return h.toolBatchUpdateConfig(args)
	case "list_config_schema":
		return h.toolListConfigSchema()
	case "export_config":
		return h.toolExportConfig()
	case "import_config":
		return h.toolImportConfig(args)
	case "set_max_iterations":
		return h.toolSetMaxIterations(args)
	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

func (h *IMMessageHandler) toolListSessions() string {
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return "当前没有活跃会话。"
	}
	var b strings.Builder
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" 任务=%s", task))
		}
		if waiting {
			b.WriteString(" [等待用户输入]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolCreateSession(args map[string]interface{}) string {
	tool, _ := args["tool"].(string)
	projectPath, _ := args["project_path"].(string)
	provider, _ := args["provider"].(string)

	var hints []string

	// Smart tool recommendation when tool is empty.
	if tool == "" && h.contextResolver != nil {
		recommended, reason := h.contextResolver.ResolveTool(projectPath, "")
		if recommended != "" {
			tool = recommended
			hints = append(hints, fmt.Sprintf("🔧 自动推荐工具: %s（%s）", tool, reason))
		}
	}
	if tool == "" {
		return "缺少 tool 参数，且无法自动推荐工具"
	}

	// Smart project detection when project_path is empty.
	if projectPath == "" && h.contextResolver != nil {
		detected, reason := h.contextResolver.ResolveProject()
		if detected != "" {
			projectPath = detected
			hints = append(hints, fmt.Sprintf("📁 自动检测项目: %s（%s）", projectPath, reason))
		}
	}

	// Pre-launch environment check.
	if h.sessionPrecheck != nil {
		result := h.sessionPrecheck.Check(tool, projectPath)
		if !result.ToolReady {
			hints = append(hints, fmt.Sprintf("⚠️ 工具预检未通过: %s", result.ToolHint))
		}
		if !result.ProjectReady {
			hints = append(hints, "⚠️ 项目路径不存在或无法访问")
		}
		if !result.ModelReady {
			hints = append(hints, fmt.Sprintf("⚠️ 模型预检未通过: %s", result.ModelHint))
		}
		if result.AllPassed {
			hints = append(hints, "✅ 环境预检全部通过")
		}
	}

	view, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool: tool, ProjectPath: projectPath, Provider: provider,
	})
	if err != nil {
		errMsg := fmt.Sprintf("创建会话失败: %s", err.Error())
		if provider != "" {
			cfg, cfgErr := h.app.LoadConfig()
			if cfgErr == nil {
				toolCfg, tcErr := remoteToolConfig(cfg, tool)
				if tcErr == nil {
					valid := validProviders(toolCfg)
					if len(valid) > 0 {
						var names []string
						for _, m := range valid {
							names = append(names, m.ModelName)
						}
						errMsg += fmt.Sprintf("\n可用服务商: %s", strings.Join(names, ", "))
					}
				}
			}
		}
		return errMsg
	}

	// Start monitoring session startup progress in background.
	if h.startupFeedback != nil {
		h.startupFeedback.WatchStartup(view.ID, func(msg string) {
			// Progress messages are logged; in a real IM context the
			// onProgress callback from the agent loop would relay these.
			fmt.Fprintf(os.Stderr, "startup_feedback[%s]: %s\n", view.ID, msg)
		})
	}

	var b strings.Builder
	for _, hint := range hints {
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("会话已创建: ID=%s 工具=%s 标题=%s\n⚠️ 你必须立即调用 get_session_output(session_id=%q) 确认会话是否正常启动，不要直接告诉用户已完成。", view.ID, view.Tool, view.Title, view.ID))
	return b.String()
}

func (h *IMMessageHandler) toolListProviders(args map[string]interface{}) string {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return "缺少 tool 参数"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %s", err.Error())
	}
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return fmt.Sprintf("不支持的工具: %s", toolName)
	}
	valid := validProviders(toolCfg)
	if len(valid) == 0 {
		return fmt.Sprintf("工具 %s 没有可用的服务商，请在桌面端配置", toolName)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("工具 %s 的可用服务商:\n", toolName))
	for _, m := range valid {
		isDefault := ""
		if strings.EqualFold(m.ModelName, toolCfg.CurrentModel) {
			isDefault = " [当前默认]"
		}
		modelId := m.ModelId
		if len(modelId) > 20 {
			modelId = modelId[:20] + "..."
		}
		b.WriteString(fmt.Sprintf("  - %s (model_id=%s)%s\n", m.ModelName, modelId, isDefault))
	}
	return b.String()
}

func (h *IMMessageHandler) toolSendInput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缺少 session_id 或 text 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return fmt.Sprintf("已发送到会话 %s。⚠️ 你必须立即调用 get_session_output(session_id=%q) 验证命令是否执行成功，不要直接告诉用户已完成。", sessionID, sessionID)
}

func (h *IMMessageHandler) toolGetSessionOutput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}

	maxLines := 30
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
		if maxLines > 100 {
			maxLines = 100
		}
	}

	session.mu.RLock()
	status := string(session.Status)
	summary := session.Summary
	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	session.mu.RUnlock()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("会话 %s 状态: %s\n", sessionID, status))
	if summary.CurrentTask != "" {
		b.WriteString(fmt.Sprintf("当前任务: %s\n", summary.CurrentTask))
	}
	if summary.ProgressSummary != "" {
		b.WriteString(fmt.Sprintf("进度: %s\n", summary.ProgressSummary))
	}
	if summary.LastResult != "" {
		b.WriteString(fmt.Sprintf("最近结果: %s\n", summary.LastResult))
	}
	if summary.LastCommand != "" {
		b.WriteString(fmt.Sprintf("最近命令: %s\n", summary.LastCommand))
	}
	if summary.WaitingForUser {
		b.WriteString("⚠️ 会话正在等待用户输入\n")
	}
	if summary.SuggestedAction != "" {
		b.WriteString(fmt.Sprintf("建议操作: %s\n", summary.SuggestedAction))
	}
	if len(rawLines) > 0 {
		start := 0
		if len(rawLines) > maxLines {
			start = len(rawLines) - maxLines
		}
		b.WriteString(fmt.Sprintf("\n--- 最近 %d 行输出 ---\n", len(rawLines)-start))
		for _, line := range rawLines[start:] {
			b.WriteString(line)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n(暂无输出)\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolGetSessionEvents(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}
	session.mu.RLock()
	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	session.mu.RUnlock()
	if len(events) == 0 {
		return fmt.Sprintf("会话 %s 暂无重要事件。", sessionID)
	}
	var b strings.Builder
	for _, ev := range events {
		b.WriteString(fmt.Sprintf("- [%s] %s: %s", ev.Severity, ev.Type, ev.Title))
		if ev.Summary != "" {
			b.WriteString(fmt.Sprintf(" — %s", ev.Summary))
		}
		if ev.RelatedFile != "" {
			b.WriteString(fmt.Sprintf(" (文件: %s)", ev.RelatedFile))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolInterruptSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.Interrupt(sessionID); err != nil {
		return fmt.Sprintf("中断失败: %s", err.Error())
	}
	return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
}

func (h *IMMessageHandler) toolKillSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.Kill(sessionID); err != nil {
		return fmt.Sprintf("终止失败: %s", err.Error())
	}
	return fmt.Sprintf("已终止会话 %s", sessionID)
}

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)

	// 如果未指定 session_id，自动选择唯一活跃会话
	if sessionID == "" && h.manager != nil {
		sessions := h.manager.List()
		if len(sessions) == 1 {
			sessionID = sessions[0].ID
		} else if len(sessions) > 1 {
			var lines []string
			lines = append(lines, "有多个活跃会话，请指定 session_id：")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				s.mu.RUnlock()
				lines = append(lines, fmt.Sprintf("- %s (工具=%s, 状态=%s)", s.ID, s.Tool, status))
			}
			return strings.Join(lines, "\n")
		} else {
			// 没有活跃会话时，直接截屏本机屏幕（不依赖 session）
			base64Data, err := h.manager.CaptureScreenshotDirect()
			if err != nil {
				return fmt.Sprintf("截图失败: %s", err.Error())
			}
			return fmt.Sprintf("[screenshot_base64]%s", base64Data)
		}
	}

	if sessionID == "" {
		return "缺少 session_id 参数，且无法自动选择会话"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.CaptureScreenshot(sessionID); err != nil {
		return fmt.Sprintf("截图失败: %s", err.Error())
	}
	return fmt.Sprintf("已请求截图。⚠️ 你必须立即调用 get_session_events(session_id=%q) 确认截图是否成功发送，不要直接告诉用户已完成。", sessionID)
}

func (h *IMMessageHandler) toolListMCPTools() string {
	var b strings.Builder
	hasAny := false

	// List local (stdio) MCP servers
	if mgr := h.app.localMCPManager; mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [本地/stdio]\n", ts.ServerName, ts.ServerID))
			for _, t := range ts.Tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	// List remote (HTTP) MCP servers
	registry := h.app.mcpRegistry
	if registry != nil {
		servers := registry.ListServers()
		for _, s := range servers {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [远程/HTTP] 状态=%s\n", s.Name, s.ID, s.HealthStatus))
			tools := registry.GetServerTools(s.ID)
			if len(tools) == 0 {
				b.WriteString("  (无工具或无法获取)\n")
				continue
			}
			for _, t := range tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	if !hasAny {
		return "没有已注册的 MCP Server"
	}
	return b.String()
}

func (h *IMMessageHandler) toolCallMCPTool(args map[string]interface{}) string {
	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if serverID == "" || toolName == "" {
		return "缺少 server_id 或 tool_name 参数"
	}
	toolArgs, _ := args["arguments"].(map[string]interface{})

	// Try local MCP manager first (stdio-based servers)
	if mgr := h.app.localMCPManager; mgr != nil && mgr.IsRunning(serverID) {
		result, err := mgr.CallTool(serverID, toolName, toolArgs)
		if err != nil {
			return fmt.Sprintf("本地 MCP 调用失败: %s", err.Error())
		}
		return result
	}

	// Fall back to remote MCP registry (HTTP-based servers)
	registry := h.app.mcpRegistry
	if registry == nil {
		return "MCP Registry 未初始化"
	}
	result, err := registry.CallTool(serverID, toolName, toolArgs)
	if err != nil {
		return fmt.Sprintf("MCP 调用失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolListSkills() string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	skills := exec.List()

	var b strings.Builder

	// Show local skills
	if len(skills) > 0 {
		b.WriteString("=== 本地已注册 Skill ===\n")
		for _, s := range skills {
			line := fmt.Sprintf("- %s [%s]: %s", s.Name, s.Status, s.Description)
			if s.Source == "hub" {
				line += fmt.Sprintf(" (来源: Hub, trust: %s)", s.TrustLevel)
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("本地没有已注册的 Skill。\n")
	}

	// If local skills are empty or few, also show Hub recommendations
	if len(skills) < 3 && h.app.skillHubClient != nil {
		recs := h.app.skillHubClient.GetRecommendations()
		if len(recs) > 0 {
			b.WriteString("\n=== SkillHub 推荐 Skill（可用 install_skill_hub 安装）===\n")
			for _, r := range recs {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (trust: %s, downloads: %d, hub: %s)\n",
					r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.HubURL))
			}
		} else {
			b.WriteString("\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n")
		}
	}

	return b.String()
}

func (h *IMMessageHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "缺少 query 参数"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureRemoteInfra()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 客户端未初始化，请检查配置中的 skill_hub_urls"
	}

	results, err := h.app.skillHubClient.Search(context.Background(), query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(results) == 0 {
		return fmt.Sprintf("在 SkillHub 上未找到与 %q 相关的 Skill", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 个 Skill：\n", len(results)))
	for _, r := range results {
		tags := ""
		if len(r.Tags) > 0 {
			tags = " [" + strings.Join(r.Tags, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("- ID: %s | %s: %s%s (trust: %s, downloads: %d, hub: %s)\n",
			r.ID, r.Name, r.Description, tags, r.TrustLevel, r.Downloads, r.HubURL))
	}
	b.WriteString("\n使用 install_skill_hub 工具安装，需提供 skill_id 和 hub_url 参数。")
	return b.String()
}

func (h *IMMessageHandler) toolInstallSkillHub(args map[string]interface{}) string {
	skillID, _ := args["skill_id"].(string)
	hubURL, _ := args["hub_url"].(string)
	if skillID == "" {
		return "缺少 skill_id 参数"
	}
	if hubURL == "" {
		return "缺少 hub_url 参数"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureRemoteInfra()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 客户端未初始化"
	}
	if h.app.skillExecutor == nil {
		return "Skill Executor 未初始化"
	}

	// Download from Hub
	entry, err := h.app.skillHubClient.Install(context.Background(), skillID, hubURL)
	if err != nil {
		return fmt.Sprintf("安装失败: %s", err.Error())
	}

	// Security review if risk assessor is available
	if h.app.riskAssessor != nil {
		assessment := h.app.riskAssessor.AssessSkill(entry, entry.TrustLevel)
		if assessment.Level == RiskCritical {
			if h.app.auditLog != nil {
				_ = h.app.auditLog.Log(AuditEntry{
					Timestamp:    time.Now(),
					Action:       AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    RiskCritical,
					PolicyAction: PolicyDeny,
					Result:       fmt.Sprintf("rejected skill %s from %s: critical risk", skillID, hubURL),
				})
			}
			return fmt.Sprintf("⚠️ Skill %q 包含高风险操作，已拒绝自动安装。风险因素: %s",
				entry.Name, strings.Join(assessment.Factors, ", "))
		}
	}

	// Register locally
	if err := h.app.skillExecutor.Register(*entry); err != nil {
		return fmt.Sprintf("注册失败: %s", err.Error())
	}

	// Audit log
	if h.app.auditLog != nil {
		_ = h.app.auditLog.Log(AuditEntry{
			Timestamp:    time.Now(),
			Action:       AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    RiskLow,
			PolicyAction: PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s (%s) from %s, trust: %s", entry.Name, skillID, hubURL, entry.TrustLevel),
		})
	}

	// Auto-run: default to true unless explicitly set to false.
	autoRun := true
	if v, ok := args["auto_run"]; ok {
		switch val := v.(type) {
		case bool:
			autoRun = val
		case string:
			autoRun = strings.EqualFold(val, "true")
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("✅ 已成功安装 Skill「%s」\n描述: %s\n来源: %s\n信任等级: %s\n",
		entry.Name, entry.Description, hubURL, entry.TrustLevel))

	if autoRun {
		b.WriteString(fmt.Sprintf("\n正在立即执行 Skill「%s」...\n", entry.Name))
		result, err := h.app.skillExecutor.Execute(entry.Name)
		if err != nil {
			b.WriteString(fmt.Sprintf("执行失败: %s\n%s", err.Error(), result))
		} else {
			b.WriteString(fmt.Sprintf("执行结果:\n%s", result))
		}
	} else {
		b.WriteString(fmt.Sprintf("\n可以使用 run_skill 工具执行，名称为: %s", entry.Name))
	}

	return b.String()
}

func (h *IMMessageHandler) toolRunSkill(args map[string]interface{}) string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缺少 name 参数"
	}
	result, err := exec.Execute(name)
	if err != nil {
		return fmt.Sprintf("Skill 执行失败: %s\n%s", err.Error(), result)
	}
	return result
}

// stringVal extracts a string value from a map, returning "" if the key is
// missing or not a string.
func stringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func (h *IMMessageHandler) toolParallelExecute(args map[string]interface{}) string {
	orch := h.app.orchestrator
	if orch == nil {
		return "Orchestrator 未初始化"
	}
	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok || len(tasksRaw) == 0 {
		return "缺少 tasks 参数"
	}
	var tasks []TaskRequest
	for _, t := range tasksRaw {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		tr := TaskRequest{
			Tool:        stringVal(tm, "tool"),
			Description: stringVal(tm, "description"),
			ProjectPath: stringVal(tm, "project_path"),
		}
		if tr.Tool == "" {
			continue
		}
		tasks = append(tasks, tr)
	}
	if len(tasks) == 0 {
		return "没有有效的任务"
	}
	result, err := orch.ExecuteParallel(tasks)
	if err != nil {
		return fmt.Sprintf("并行执行失败: %s", err.Error())
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %s: %s\n", result.TaskID, result.Summary))
	for key, sr := range result.Results {
		b.WriteString(fmt.Sprintf("- %s: tool=%s status=%s", key, sr.Tool, sr.Status))
		if sr.SessionID != "" {
			b.WriteString(fmt.Sprintf(" session=%s", sr.SessionID))
		}
		if sr.Error != "" {
			b.WriteString(fmt.Sprintf(" error=%s", sr.Error))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolRecommendTool(args map[string]interface{}) string {
	selector := h.app.toolSelector
	if selector == nil {
		return "ToolSelector 未初始化"
	}
	desc, _ := args["task_description"].(string)
	if desc == "" {
		return "缺少 task_description 参数"
	}
	// Build list of installed tools by checking if their binaries are on PATH.
	var installed []string
	for _, tool := range []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	name, reason := selector.Recommend(desc, installed)
	return fmt.Sprintf("推荐工具: %s\n理由: %s", name, reason)
}

// ---------------------------------------------------------------------------
// 本机直接操作工具 (bash, read_file, write_file, list_directory)
// ---------------------------------------------------------------------------

const (
	bashDefaultTimeout = 30
	bashMaxTimeout     = 120
	readFileMaxLines   = 200
	writeFileMaxSize   = 1 << 20 // 1 MB
)

// resolvePath resolves a path, expanding ~ and making relative paths relative
// to the user's home directory.
func resolvePath(p string) string {
	if p == "" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p)
	}
	return filepath.Clean(p)
}

func (h *IMMessageHandler) toolBash(args map[string]interface{}, onProgress ProgressCallback) string {
	command, _ := args["command"].(string)
	if command == "" {
		return "缺少 command 参数"
	}

	timeout := bashDefaultTimeout
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > bashMaxTimeout {
			timeout = bashMaxTimeout
		}
	}

	workDir := resolvePath(stringVal(args, "working_dir"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

	// Start the command and send periodic heartbeats for long-running ops.
	err := cmd.Start()
	if err != nil {
		return fmt.Sprintf("[错误] 命令启动失败: %v", err)
	}

	// Heartbeat goroutine: send progress every 30s while the command runs.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		elapsed := 0
		for {
			select {
			case <-ticker.C:
				elapsed += 30
				// Truncate command for display.
				displayCmd := command
				if len(displayCmd) > 60 {
					displayCmd = displayCmd[:60] + "…"
				}
				if onProgress != nil {
					onProgress(fmt.Sprintf("⏳ 命令仍在执行中（已 %ds）: %s", elapsed, displayCmd))
				}
			case <-done:
				return
			}
		}
	}()

	err = cmd.Wait()
	close(done)

	var b strings.Builder
	if stdout.Len() > 0 {
		out := stdout.String()
		if len(out) > 8192 {
			out = out[:8192] + "\n... (输出已截断)"
		}
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		errOut := stderr.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n... (错误输出已截断)"
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(errOut)
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[错误] 命令超时（%d 秒）", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[错误] 退出码: %v", err))
		}
	}

	if b.Len() == 0 {
		return "(命令执行完成，无输出)"
	}
	return b.String()
}

func (h *IMMessageHandler) toolReadFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，请使用 list_directory 工具", absPath)
	}

	maxLines := readFileMaxLines
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取失败: %s", err.Error())
	}

	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		return strings.Join(lines, "") + fmt.Sprintf("\n... (已截断，共 %d 行，显示前 %d 行)", len(strings.SplitAfter(string(data), "\n")), maxLines)
	}
	return string(data)
}

func (h *IMMessageHandler) toolWriteFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if p == "" || content == "" {
		return "缺少 path 或 content 参数"
	}
	if len(content) > writeFileMaxSize {
		return fmt.Sprintf("内容过大（%d 字节），最大允许 %d 字节", len(content), writeFileMaxSize)
	}

	absPath := resolvePath(p)

	// 自动创建父目录
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf("创建目录失败: %s", err.Error())
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Sprintf("写入失败: %s", err.Error())
	}

	// 验证写入
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("写入后验证失败: %s", err.Error())
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", absPath, info.Size())
}

func (h *IMMessageHandler) toolListDirectory(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
	}
	if !info.IsDir() {
		return fmt.Sprintf("%s 不是目录", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Sprintf("读取目录失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("目录: %s（共 %d 项）\n", absPath, len(entries)))
	shown := 0
	for _, entry := range entries {
		if shown >= 100 {
			b.WriteString(fmt.Sprintf("... 还有 %d 项未显示\n", len(entries)-shown))
			break
		}
		info, _ := entry.Info()
		if entry.IsDir() {
			b.WriteString(fmt.Sprintf("  📁 %s/\n", entry.Name()))
		} else if info != nil {
			b.WriteString(fmt.Sprintf("  📄 %s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			b.WriteString(fmt.Sprintf("  📄 %s\n", entry.Name()))
		}
		shown++
	}
	return b.String()
}

const sendFileMaxSize = 200 << 20 // 200 MB — large files are handled by plugin-level fallback (temp URL)

func (h *IMMessageHandler) toolSendFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，不能作为文件发送", absPath)
	}
	if info.Size() > sendFileMaxSize {
		return fmt.Sprintf("文件过大（%d 字节），最大允许 %d 字节", info.Size(), sendFileMaxSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %s", err.Error())
	}

	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		fileName = filepath.Base(absPath)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	// Use | as delimiter to avoid conflicts with : in filenames or MIME types.
	return fmt.Sprintf("[file_base64|%s|%s]%s", fileName, mimeType, b64)
}

func (h *IMMessageHandler) toolOpen(args map[string]interface{}) string {
	target, _ := args["target"].(string)
	if target == "" {
		return "缺少 target 参数"
	}

	// Detect URLs (http, https, file, mailto, etc.)
	isURL := strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
	if !isURL {
		target = resolvePath(target)
		// Verify the path exists before attempting to open.
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use rundll32 url.dll,FileProtocolHandler — opens files/URLs with
		// the default handler without spawning a visible console window.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("打开失败: %s", err.Error())
	}

	// Don't wait for the process — it's a GUI application.
	go cmd.Wait()

	if isURL {
		return fmt.Sprintf("已用默认浏览器打开: %s", target)
	}
	return fmt.Sprintf("已用默认程序打开: %s", target)
}

// ---------------------------------------------------------------------------
// Memory Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSaveMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "长期记忆未初始化"
	}

	content := stringVal(args, "content")
	if content == "" {
		return "缺少 content 参数"
	}
	category := stringVal(args, "category")
	if category == "" {
		category = "user_fact"
	}

	var tags []string
	if rawTags, ok := args["tags"]; ok {
		if tagSlice, ok := rawTags.([]interface{}); ok {
			for _, t := range tagSlice {
				if s, ok := t.(string); ok && s != "" {
					tags = append(tags, s)
				}
			}
		}
	}

	entry := MemoryEntry{
		Content:  content,
		Category: MemoryCategory(category),
		Tags:     tags,
	}
	if err := h.memoryStore.Save(entry); err != nil {
		return fmt.Sprintf("保存记忆失败: %s", err.Error())
	}

	summary := content
	if len(summary) > 50 {
		summary = summary[:50] + "..."
	}
	return fmt.Sprintf("已保存记忆: %s", summary)
}

func (h *IMMessageHandler) toolListMemories(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "长期记忆未初始化"
	}

	category := MemoryCategory(stringVal(args, "category"))
	keyword := stringVal(args, "keyword")

	entries := h.memoryStore.List(category, keyword)
	if len(entries) == 0 {
		return "没有找到匹配的记忆条目。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条记忆:\n", len(entries)))
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("- [%s] (%s) %s", e.ID, e.Category, e.Content))
		if len(e.Tags) > 0 {
			b.WriteString(fmt.Sprintf(" 标签=%v", e.Tags))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolDeleteMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "长期记忆未初始化"
	}

	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}

	if err := h.memoryStore.Delete(id); err != nil {
		return fmt.Sprintf("删除记忆失败: %s", err.Error())
	}
	return fmt.Sprintf("已删除记忆: %s", id)
}

// ---------------------------------------------------------------------------
// Template Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "name")
	tool := stringVal(args, "tool")
	if name == "" || tool == "" {
		return "缺少 name 或 tool 参数"
	}

	tpl := SessionTemplate{
		Name:        name,
		Tool:        tool,
		ProjectPath: stringVal(args, "project_path"),
		ModelConfig: stringVal(args, "model_config"),
	}

	// Parse yolo_mode (can arrive as bool or string).
	if yolo, ok := args["yolo_mode"].(bool); ok {
		tpl.YoloMode = yolo
	} else if yoloStr, ok := args["yolo_mode"].(string); ok {
		tpl.YoloMode = yoloStr == "true"
	}

	if err := h.templateManager.Create(tpl); err != nil {
		return fmt.Sprintf("创建模板失败: %s", err.Error())
	}
	return fmt.Sprintf("模板已创建: %s（工具=%s, 项目=%s）", name, tool, tpl.ProjectPath)
}

func (h *IMMessageHandler) toolListTemplates() string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	templates := h.templateManager.List()
	if len(templates) == 0 {
		return "当前没有会话模板。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个模板:\n", len(templates)))
	for _, t := range templates {
		b.WriteString(fmt.Sprintf("- %s: 工具=%s 项目=%s", t.Name, t.Tool, t.ProjectPath))
		if t.ModelConfig != "" {
			b.WriteString(fmt.Sprintf(" 模型=%s", t.ModelConfig))
		}
		if t.YoloMode {
			b.WriteString(" [Yolo]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolLaunchTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "template_name")
	if name == "" {
		return "缺少 template_name 参数"
	}

	tpl, err := h.templateManager.Get(name)
	if err != nil {
		return fmt.Sprintf("获取模板失败: %s", err.Error())
	}

	// Build args from template config and delegate to toolCreateSession.
	sessionArgs := map[string]interface{}{
		"tool":         tpl.Tool,
		"project_path": tpl.ProjectPath,
	}
	return h.toolCreateSession(sessionArgs)
}

// ---------------------------------------------------------------------------
// Config Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolGetConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	result, err := h.configManager.GetConfig(section)
	if err != nil {
		return fmt.Sprintf("读取配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	key := stringVal(args, "key")
	value := stringVal(args, "value")
	if section == "" || key == "" {
		return "缺少 section 或 key 参数"
	}

	oldValue, err := h.configManager.UpdateConfig(section, key, value)
	if err != nil {
		return fmt.Sprintf("修改配置失败: %s", err.Error())
	}
	return fmt.Sprintf("配置已更新: %s.%s\n旧值: %s\n新值: %s", section, key, oldValue, value)
}

func (h *IMMessageHandler) toolBatchUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	changesStr := stringVal(args, "changes")
	if changesStr == "" {
		return "缺少 changes 参数"
	}

	var changes []ConfigChange
	if err := json.Unmarshal([]byte(changesStr), &changes); err != nil {
		return fmt.Sprintf("解析 changes 参数失败: %s", err.Error())
	}
	if len(changes) == 0 {
		return "changes 列表为空"
	}

	if err := h.configManager.BatchUpdate(changes); err != nil {
		return fmt.Sprintf("批量更新配置失败: %s", err.Error())
	}
	return fmt.Sprintf("批量更新成功，共应用 %d 项变更", len(changes))
}

func (h *IMMessageHandler) toolListConfigSchema() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.SchemaJSON()
	if err != nil {
		return fmt.Sprintf("获取配置 Schema 失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolExportConfig() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.ExportConfig()
	if err != nil {
		return fmt.Sprintf("导出配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolImportConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	jsonData := stringVal(args, "json_data")
	if jsonData == "" {
		return "缺少 json_data 参数"
	}

	report, err := h.configManager.ImportConfig(jsonData)
	if err != nil {
		return fmt.Sprintf("导入配置失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("配置导入完成: 应用 %d 项, 跳过 %d 项", report.Applied, report.Skipped))
	if len(report.Warnings) > 0 {
		b.WriteString("\n警告:")
		for _, w := range report.Warnings {
			b.WriteString(fmt.Sprintf("\n  - %s", w))
		}
	}
	return b.String()
}

// toolSetMaxIterations allows the agent to dynamically adjust the max
// iterations for the current conversation loop. This does NOT change the
// persisted config — it only affects the in-flight loop.
func (h *IMMessageHandler) toolSetMaxIterations(args map[string]interface{}) string {
	n, ok := args["max_iterations"].(float64)
	if !ok || n < 1 {
		return "缺少或无效的 max_iterations 参数（需要 1-200 的整数）"
	}
	limit := int(n)
	if limit > 200 {
		limit = 200
	}
	reason := stringVal(args, "reason")
	h.loopMaxOverride = limit
	if reason != "" {
		return fmt.Sprintf("✅ 已将当前对话最大轮数调整为 %d（原因: %s）", limit, reason)
	}
	return fmt.Sprintf("✅ 已将当前对话最大轮数调整为 %d", limit)
}
