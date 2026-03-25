package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ds2api/internal/auth"
	"ds2api/internal/sse"
	"ds2api/internal/util"
)

func (h *Handler) GetResponseByID(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.DetermineCaller(r)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, err.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "response_id"))
	if id == "" {
		writeOpenAIError(w, http.StatusBadRequest, "response_id is required.")
		return
	}
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	st := h.getResponseStore()
	item, ok := st.get(owner, id)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "Response not found.")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	r = r.WithContext(auth.WithAuth(r.Context(), a))
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	stdReq, err := normalizeOpenAIResponsesRequest(h.Store, req)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := h.DS.CreateSession(r.Context(), a, 3)
	if err != nil {
		if a.UseConfigToken {
			writeOpenAIError(w, http.StatusUnauthorized, "Account token is invalid. Please re-login the account in admin.")
		} else {
			writeOpenAIError(w, http.StatusUnauthorized, "Invalid token. If this should be a DS2API key, add it to config.keys first.")
		}
		return
	}
	pow, err := h.DS.GetPow(r.Context(), a, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, "Failed to get PoW (invalid token or unknown error).")
		return
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(r.Context(), a, payload, pow, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "Failed to get completion.")
		return
	}

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if stdReq.Stream {
		h.handleResponsesStream(w, r, resp, owner, responseID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames)
		return
	}
	h.handleResponsesNonStream(w, resp, owner, responseID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.ToolNames)
}

func (h *Handler) handleResponsesNonStream(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, thinkingEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)
	responseObj := util.BuildOpenAIResponseObject(responseID, model, finalPrompt, result.Thinking, result.Text, toolNames)
	h.getResponseStore().put(owner, responseID, responseObj)
	writeJSON(w, http.StatusOK, responseObj)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, resp *http.Response, owner, responseID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	canFlush := rc.Flush() == nil

	sendEvent := func(event string, payload map[string]any) {
		b, _ := json.Marshal(payload)
		_, _ = w.Write([]byte("event: " + event + "\n"))
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		if canFlush {
			_ = rc.Flush()
		}
	}

	sendEvent("response.created", util.BuildOpenAIResponsesCreatedPayload(responseID, model))

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	parsedLines, done := sse.StartParsedLinePump(r.Context(), resp.Body, thinkingEnabled, initialType)
	bufferToolContent := len(toolNames) > 0 && h.toolcallFeatureMatchEnabled()
	emitEarlyToolDeltas := h.toolcallEarlyEmitHighConfidence()
	var sieve toolStreamSieveState
	thinking := strings.Builder{}
	text := strings.Builder{}
	toolCallsEmitted := false
	streamToolCallIDs := map[int]string{}

	finalize := func() {
		finalThinking := thinking.String()
		finalText := text.String()
		if bufferToolContent {
			for _, evt := range flushToolSieve(&sieve, toolNames) {
				if evt.Content != "" {
					sendEvent("response.output_text.delta", util.BuildOpenAIResponsesTextDeltaPayload(responseID, evt.Content))
				}
				if len(evt.ToolCalls) > 0 {
					toolCallsEmitted = true
					sendEvent("response.output_tool_call.done", util.BuildOpenAIResponsesToolCallDonePayload(responseID, util.FormatOpenAIStreamToolCalls(evt.ToolCalls)))
				}
			}
		}
		obj := util.BuildOpenAIResponseObject(responseID, model, finalPrompt, finalThinking, finalText, toolNames)
		if toolCallsEmitted {
			obj["status"] = "completed"
		}
		h.getResponseStore().put(owner, responseID, obj)
		sendEvent("response.completed", util.BuildOpenAIResponsesCompletedPayload(obj))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if canFlush {
			_ = rc.Flush()
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case parsed, ok := <-parsedLines:
			if !ok {
				_ = <-done
				finalize()
				return
			}
			if !parsed.Parsed {
				continue
			}
			if parsed.ContentFilter || parsed.ErrorMessage != "" || parsed.Stop {
				finalize()
				return
			}
			for _, p := range parsed.Parts {
				if p.Text == "" {
					continue
				}
				if p.Type != "thinking" && searchEnabled && sse.IsCitation(p.Text) {
					continue
				}
				if p.Type == "thinking" {
					if !thinkingEnabled {
						continue
					}
					thinking.WriteString(p.Text)
					sendEvent("response.reasoning.delta", util.BuildOpenAIResponsesReasoningDeltaPayload(responseID, p.Text))
					continue
				}
				text.WriteString(p.Text)
				if !bufferToolContent {
					sendEvent("response.output_text.delta", util.BuildOpenAIResponsesTextDeltaPayload(responseID, p.Text))
					continue
				}
				for _, evt := range processToolSieveChunk(&sieve, p.Text, toolNames) {
					if evt.Content != "" {
						sendEvent("response.output_text.delta", util.BuildOpenAIResponsesTextDeltaPayload(responseID, evt.Content))
					}
					if len(evt.ToolCallDeltas) > 0 {
						if !emitEarlyToolDeltas {
							continue
						}
						toolCallsEmitted = true
						sendEvent("response.output_tool_call.delta", util.BuildOpenAIResponsesToolCallDeltaPayload(responseID, formatIncrementalStreamToolCallDeltas(evt.ToolCallDeltas, streamToolCallIDs)))
					}
					if len(evt.ToolCalls) > 0 {
						toolCallsEmitted = true
						sendEvent("response.output_tool_call.done", util.BuildOpenAIResponsesToolCallDonePayload(responseID, util.FormatOpenAIStreamToolCalls(evt.ToolCalls)))
					}
				}
			}
		}
	}
}

func responsesMessagesFromRequest(req map[string]any) []any {
	if msgs, ok := req["messages"].([]any); ok && len(msgs) > 0 {
		return prependInstructionMessage(msgs, req["instructions"])
	}
	if rawInput, ok := req["input"]; ok {
		if msgs := normalizeResponsesInputAsMessages(rawInput); len(msgs) > 0 {
			return prependInstructionMessage(msgs, req["instructions"])
		}
	}
	return nil
}

func prependInstructionMessage(messages []any, instructions any) []any {
	sys, _ := instructions.(string)
	sys = strings.TrimSpace(sys)
	if sys == "" {
		return messages
	}
	out := make([]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": sys})
	out = append(out, messages...)
	return out
}

func normalizeResponsesInputAsMessages(input any) []any {
	switch v := input.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []any{map[string]any{"role": "user", "content": v}}
	case []any:
		if len(v) == 0 {
			return nil
		}
		// If caller already provides role-shaped items, keep as-is.
		if first, ok := v[0].(map[string]any); ok {
			if _, hasRole := first["role"]; hasRole {
				return v
			}
		}
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["type"].(string); strings.EqualFold(strings.TrimSpace(t), "input_text") {
					if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
						parts = append(parts, txt)
						continue
					}
				}
			}
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []any{map[string]any{"role": "user", "content": strings.Join(parts, "\n")}}
	case map[string]any:
		if txt, _ := v["text"].(string); strings.TrimSpace(txt) != "" {
			return []any{map[string]any{"role": "user", "content": txt}}
		}
		if content, ok := v["content"].(string); ok && strings.TrimSpace(content) != "" {
			return []any{map[string]any{"role": "user", "content": content}}
		}
	}
	return nil
}
