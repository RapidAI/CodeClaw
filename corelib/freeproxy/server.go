package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server is the OpenAI-compatible HTTP proxy server backed by 当贝 AI.
type Server struct {
	addr     string
	auth     *AuthStore
	client   *DangbeiClient
	mu       sync.Mutex // serialize completion requests (one at a time)
	listener net.Listener
	srv      *http.Server

	modelMu      sync.RWMutex // protects defaultModel
	defaultModel string       // user-selected model
}

// NewServer creates a new proxy server.
func NewServer(addr, configDir string) *Server {
	auth := NewAuthStore(configDir)
	auth.Load()
	return &Server{
		addr:   addr,
		auth:   auth,
		client: NewDangbeiClient(auth),
	}
}

// Auth returns the underlying AuthStore for external login flows.
func (s *Server) Auth() *AuthStore { return s.auth }

// Client returns the underlying DangbeiClient.
func (s *Server) Client() *DangbeiClient { return s.client }

// SetDefaultModel sets the default model used when the request model is
// "free-proxy" or empty. Thread-safe.
func (s *Server) SetDefaultModel(model string) {
	s.modelMu.Lock()
	s.defaultModel = model
	s.modelMu.Unlock()
}

// getDefaultModel returns the user-selected default model, falling back to deepseek_r1.
func (s *Server) getDefaultModel() string {
	s.modelMu.RLock()
	m := s.defaultModel
	s.modelMu.RUnlock()
	if m == "" {
		return "deepseek_r1"
	}
	return m
}

// Start starts the HTTP server. It blocks until the server is stopped.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/health", s.handleHealth)

	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}

	s.srv = &http.Server{Handler: mux}
	log.Printf("[freeproxy] listening on %s (当贝 AI backend)", s.listener.Addr())

	go func() {
		<-ctx.Done()
		s.srv.Shutdown(context.Background())
	}()

	if err := s.srv.Serve(s.listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.srv != nil {
		s.srv.Shutdown(context.Background())
	}
}

// ── OpenAI-compatible request/response types ──

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Tools    []interface{} `json:"tools,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message,omitempty"`
	Delta        chatMessage `json:"delta,omitempty"`
	FinishReason *string     `json:"finish_reason"`
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := AvailableModels()
	var data []map[string]interface{}
	data = append(data, map[string]interface{}{
		"id": "free-proxy", "object": "model", "owned_by": "dangbei",
	})
	for _, m := range models {
		data = append(data, map[string]interface{}{
			"id": m.ID, "object": "model", "owned_by": "dangbei",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": data})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.auth.HasAuth() {
		writeError(w, http.StatusUnauthorized, "未登录当贝 AI，请先在 MaClaw 设置中完成登录")
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Combine messages into a single prompt
	var prompt strings.Builder

	// Inject tool system prompt before other messages so the model sees it first
	hasTools := len(req.Tools) > 0
	if hasTools {
		toolPrompt := GenerateToolSystemPrompt(req.Tools)
		if toolPrompt != "" {
			prompt.WriteString("[System] " + toolPrompt + "\n\n")
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			prompt.WriteString("[System] " + m.Content + "\n\n")
		case "user":
			prompt.WriteString(m.Content + "\n")
		case "assistant":
			prompt.WriteString("[Previous assistant response] " + m.Content + "\n\n")
		}
	}

	modelClass := req.Model
	if modelClass == "" || modelClass == "free-proxy" {
		modelClass = s.getDefaultModel()
	}

	ctx := r.Context()

	// Create a conversation for this request
	conversationID, err := s.client.CreateSession(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "create conversation: "+err.Error())
		return
	}
	defer func() {
		go func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer dcancel()
			s.client.DeleteSession(dctx, conversationID)
		}()
	}()

	// Serialize completions to avoid rate limits
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "request cancelled while waiting in queue")
		return
	}

	cr := CompletionRequest{
		ConversationID: conversationID,
		Prompt:         prompt.String(),
		ModelClass:     modelClass,
	}

	if req.Stream {
		s.handleStream(ctx, w, cr, modelClass, hasTools)
	} else {
		s.handleNonStream(ctx, w, cr, modelClass, hasTools)
	}
}

func (s *Server) handleNonStream(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string, hasTools bool) {
	fullText, _, err := s.client.StreamCompletion(ctx, cr, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "completion error: "+err.Error())
		return
	}

	stop := "stop"
	choice := chatChoice{Index: 0, FinishReason: &stop}

	// Check for tool calls in the response
	if hasTools && HasToolCalls(fullText) {
		toolCalls := ParseToolCalls(fullText)
		if len(toolCalls) > 0 {
			cleanText := RemoveToolCallBlocks(fullText)
			toolStop := "tool_calls"
			choice.FinishReason = &toolStop
			choice.Message = chatMessage{Role: "assistant", Content: cleanText}
			choice.ToolCalls = toolCalls
		} else {
			choice.Message = chatMessage{Role: "assistant", Content: fullText}
		}
	} else {
		choice.Message = chatMessage{Role: "assistant", Content: fullText}
	}

	resp := chatResponse{
		ID:      fmt.Sprintf("fp-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{choice},
		Usage: chatUsage{
			PromptTokens:     len(cr.Prompt) / 4,
			CompletionTokens: len(fullText) / 4,
			TotalTokens:      (len(cr.Prompt) + len(fullText)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStream(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string, hasTools bool) {
	// When tools are present, we must buffer the full response to detect
	// tool_call blocks before sending anything to the client.
	if hasTools {
		s.handleStreamWithTools(ctx, w, cr, model)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	id := fmt.Sprintf("fp-%d", time.Now().UnixMilli())

	sendChunk := func(content string, finish bool) {
		chunk := chatResponse{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []chatChoice{{Index: 0, Delta: chatMessage{Role: "assistant", Content: content}}},
		}
		if finish {
			stop := "stop"
			chunk.Choices[0].FinishReason = &stop
			chunk.Choices[0].Delta = chatMessage{}
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	_, _, err := s.client.StreamCompletion(ctx, cr, func(token string) {
		sendChunk(token, false)
	})
	if err != nil {
		sendChunk(fmt.Sprintf("\n[stream error: %v]", err), false)
	}

	sendChunk("", true)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleStreamWithTools buffers the full completion, checks for tool calls,
// then emits the result as SSE. This is necessary because tool_call blocks
// can only be detected after the full response is available.
func (s *Server) handleStreamWithTools(ctx context.Context, w http.ResponseWriter, cr CompletionRequest, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	fullText, _, err := s.client.StreamCompletion(ctx, cr, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "completion error: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := fmt.Sprintf("fp-%d", time.Now().UnixMilli())

	if HasToolCalls(fullText) {
		toolCalls := ParseToolCalls(fullText)
		if len(toolCalls) > 0 {
			cleanText := RemoveToolCallBlocks(fullText)
			// Emit text content if any
			if cleanText != "" {
				chunk := chatResponse{
					ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
					Choices: []chatChoice{{Index: 0, Delta: chatMessage{Role: "assistant", Content: cleanText}}},
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
			// Emit tool calls chunk
			toolStop := "tool_calls"
			chunk := chatResponse{
				ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
				Choices: []chatChoice{{Index: 0, Delta: chatMessage{}, FinishReason: &toolStop, ToolCalls: toolCalls}},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}

	// No tool calls — emit as normal text stream
	stop := "stop"
	chunk := chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []chatChoice{{Index: 0, Delta: chatMessage{Role: "assistant", Content: fullText}}},
	}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	finishChunk := chatResponse{
		ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model,
		Choices: []chatChoice{{Index: 0, Delta: chatMessage{}, FinishReason: &stop}},
	}
	data, _ = json.Marshal(finishChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "freeproxy_error",
			"code":    code,
		},
	})
}
