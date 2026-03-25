package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	"ds2api/internal/sse"
	"ds2api/internal/util"
)

// writeJSON is a package-internal alias to avoid mass-renaming all call-sites.
var writeJSON = util.WriteJSON

type Handler struct {
	Store *config.Store
	Auth  *auth.Resolver
	DS    *deepseek.Client
}

var (
	claudeStreamPingInterval    = time.Duration(deepseek.KeepAliveTimeout) * time.Second
	claudeStreamIdleTimeout     = time.Duration(deepseek.StreamIdleTimeout) * time.Second
	claudeStreamMaxKeepaliveCnt = deepseek.MaxKeepaliveCount
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/anthropic/v1/models", h.ListModels)
	r.Post("/anthropic/v1/messages", h.Messages)
	r.Post("/anthropic/v1/messages/count_tokens", h.CountTokens)
}

func (h *Handler) ListModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, config.ClaudeModelsResponse())
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("anthropic-version")) == "" {
		r.Header.Set("anthropic-version", "2023-06-01")
	}
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeClaudeError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClaudeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	norm, err := normalizeClaudeRequest(h.Store, req)
	if err != nil {
		writeClaudeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stdReq := norm.Standard

	sessionID, err := h.DS.CreateSession(r.Context(), a, 3)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, "invalid token.")
		return
	}
	pow, err := h.DS.GetPow(r.Context(), a, 3)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, "Failed to get PoW")
		return
	}
	requestPayload := stdReq.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(r.Context(), a, requestPayload, pow, 3)
	if err != nil {
		writeClaudeError(w, http.StatusInternalServerError, "Failed to get Claude response.")
		return
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		writeClaudeError(w, http.StatusInternalServerError, string(body))
		return
	}

	if stdReq.Stream {
		h.handleClaudeStreamRealtime(w, r, resp, stdReq.ResponseModel, norm.NormalizedMessages, stdReq.Thinking, stdReq.Search, stdReq.ToolNames)
		return
	}
	result := sse.CollectStream(resp, stdReq.Thinking, true)
	respBody := util.BuildClaudeMessageResponse(
		fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		stdReq.ResponseModel,
		norm.NormalizedMessages,
		result.Thinking,
		result.Text,
		stdReq.ToolNames,
	)
	writeJSON(w, http.StatusOK, respBody)
}

func (h *Handler) CountTokens(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	defer h.Auth.Release(a)

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClaudeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	model, _ := req["model"].(string)
	messages, _ := req["messages"].([]any)
	if model == "" || len(messages) == 0 {
		writeClaudeError(w, http.StatusBadRequest, "Request must include 'model' and 'messages'.")
		return
	}
	inputTokens := 0
	if sys, ok := req["system"].(string); ok {
		inputTokens += util.EstimateTokens(sys)
	}
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		inputTokens += 2
		inputTokens += util.EstimateTokens(extractMessageContent(msg["content"]))
	}
	if tools, ok := req["tools"].([]any); ok {
		for _, t := range tools {
			b, _ := json.Marshal(t)
			inputTokens += util.EstimateTokens(string(b))
		}
	}
	if inputTokens < 1 {
		inputTokens = 1
	}
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": inputTokens})
}

func (h *Handler) handleClaudeStreamRealtime(w http.ResponseWriter, r *http.Request, resp *http.Response, model string, messages []any, thinkingEnabled, searchEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeClaudeError(w, http.StatusInternalServerError, string(body))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	canFlush := rc.Flush() == nil
	if !canFlush {
		config.Logger.Warn("[claude_stream] response writer does not support flush; streaming may be buffered")
	}
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		_, _ = w.Write([]byte("event: "))
		_, _ = w.Write([]byte(event))
		_, _ = w.Write([]byte("\n"))
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		if canFlush {
			_ = rc.Flush()
		}
	}
	sendError := func(message string) {
		msg := strings.TrimSpace(message)
		if msg == "" {
			msg = "upstream stream error"
		}
		send("error", map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "api_error",
				"message": msg,
				"code":    "internal_error",
				"param":   nil,
			},
		})
	}

	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	inputTokens := util.EstimateTokens(fmt.Sprintf("%v", messages))
	send("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": inputTokens, "output_tokens": 0},
		},
	})

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	parsedLines, done := sse.StartParsedLinePump(r.Context(), resp.Body, thinkingEnabled, initialType)
	bufferToolContent := len(toolNames) > 0
	hasContent := false
	lastContent := time.Now()
	keepaliveCount := 0

	thinking := strings.Builder{}
	text := strings.Builder{}

	nextBlockIndex := 0
	thinkingBlockOpen := false
	thinkingBlockIndex := -1
	textBlockOpen := false
	textBlockIndex := -1
	ended := false

	closeThinkingBlock := func() {
		if !thinkingBlockOpen {
			return
		}
		send("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": thinkingBlockIndex,
		})
		thinkingBlockOpen = false
		thinkingBlockIndex = -1
	}
	closeTextBlock := func() {
		if !textBlockOpen {
			return
		}
		send("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": textBlockIndex,
		})
		textBlockOpen = false
		textBlockIndex = -1
	}

	finalize := func(stopReason string) {
		if ended {
			return
		}
		ended = true

		closeThinkingBlock()
		closeTextBlock()

		finalThinking := thinking.String()
		finalText := text.String()

		if bufferToolContent {
			detected := util.ParseToolCalls(finalText, toolNames)
			if len(detected) > 0 {
				stopReason = "tool_use"
				for i, tc := range detected {
					idx := nextBlockIndex + i
					send("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": idx,
						"content_block": map[string]any{
							"type":  "tool_use",
							"id":    fmt.Sprintf("toolu_%d_%d", time.Now().Unix(), idx),
							"name":  tc.Name,
							"input": tc.Input,
						},
					})
					send("content_block_stop", map[string]any{
						"type":  "content_block_stop",
						"index": idx,
					})
				}
				nextBlockIndex += len(detected)
			} else if finalText != "" {
				idx := nextBlockIndex
				nextBlockIndex++
				send("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				})
				send("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]any{
						"type": "text_delta",
						"text": finalText,
					},
				})
				send("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": idx,
				})
			}
		}

		outputTokens := util.EstimateTokens(finalThinking) + util.EstimateTokens(finalText)
		send("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"output_tokens": outputTokens,
			},
		})
		send("message_stop", map[string]any{"type": "message_stop"})
	}

	pingTicker := time.NewTicker(claudeStreamPingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-pingTicker.C:
			if !hasContent {
				keepaliveCount++
				if keepaliveCount >= claudeStreamMaxKeepaliveCnt {
					finalize("end_turn")
					return
				}
			}
			if hasContent && time.Since(lastContent) > claudeStreamIdleTimeout {
				finalize("end_turn")
				return
			}
			send("ping", map[string]any{"type": "ping"})
		case parsed, ok := <-parsedLines:
			if !ok {
				if err := <-done; err != nil {
					sendError(err.Error())
					return
				}
				finalize("end_turn")
				return
			}
			if !parsed.Parsed {
				continue
			}
			if parsed.ErrorMessage != "" {
				sendError(parsed.ErrorMessage)
				return
			}
			if parsed.Stop {
				finalize("end_turn")
				return
			}

			for _, p := range parsed.Parts {
				if p.Text == "" {
					continue
				}
				if p.Type != "thinking" && searchEnabled && sse.IsCitation(p.Text) {
					continue
				}

				hasContent = true
				lastContent = time.Now()
				keepaliveCount = 0

				if p.Type == "thinking" {
					if !thinkingEnabled {
						continue
					}
					thinking.WriteString(p.Text)
					closeTextBlock()
					if !thinkingBlockOpen {
						thinkingBlockIndex = nextBlockIndex
						nextBlockIndex++
						send("content_block_start", map[string]any{
							"type":  "content_block_start",
							"index": thinkingBlockIndex,
							"content_block": map[string]any{
								"type":     "thinking",
								"thinking": "",
							},
						})
						thinkingBlockOpen = true
					}
					send("content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": thinkingBlockIndex,
						"delta": map[string]any{
							"type":     "thinking_delta",
							"thinking": p.Text,
						},
					})
					continue
				}

				text.WriteString(p.Text)
				if bufferToolContent {
					continue
				}
				closeThinkingBlock()
				if !textBlockOpen {
					textBlockIndex = nextBlockIndex
					nextBlockIndex++
					send("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": textBlockIndex,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					})
					textBlockOpen = true
				}
				send("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": textBlockIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": p.Text,
					},
				})
			}
		}
	}
}

func writeClaudeError(w http.ResponseWriter, status int, message string) {
	code := "invalid_request"
	switch status {
	case http.StatusUnauthorized:
		code = "authentication_failed"
	case http.StatusTooManyRequests:
		code = "rate_limit_exceeded"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusInternalServerError:
		code = "internal_error"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
			"code":    code,
			"param":   nil,
		},
	})
}

func normalizeClaudeMessages(messages []any) []any {
	out := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		copied := cloneMap(msg)
		switch content := msg["content"].(type) {
		case []any:
			parts := make([]string, 0, len(content))
			for _, block := range content {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				typeStr, _ := b["type"].(string)
				if typeStr == "text" {
					if t, ok := b["text"].(string); ok {
						parts = append(parts, t)
					}
				}
				if typeStr == "tool_result" {
					parts = append(parts, fmt.Sprintf("%v", b["content"]))
				}
			}
			copied["content"] = strings.Join(parts, "\n")
		}
		out = append(out, copied)
	}
	return out
}

func buildClaudeToolPrompt(tools []any) string {
	parts := []string{"You are Claude, a helpful AI assistant. You have access to these tools:"}
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		desc, _ := m["description"].(string)
		schema, _ := json.Marshal(m["input_schema"])
		parts = append(parts, fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s", name, desc, schema))
	}
	parts = append(parts, "When you need to use tools, you can call multiple tools in one response. Output ONLY JSON like {\"tool_calls\":[{\"name\":\"tool\",\"input\":{}}]}")
	return strings.Join(parts, "\n\n")
}

func hasSystemMessage(messages []any) bool {
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if ok && msg["role"] == "system" {
			return true
		}
	}
	return false
}

func extractClaudeToolNames(tools []any) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

func toMessageMaps(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func extractMessageContent(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, it := range x {
			parts = append(parts, fmt.Sprintf("%v", it))
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", x)
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
