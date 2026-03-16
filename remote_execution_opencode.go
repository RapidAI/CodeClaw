package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenCodeSDKExecutionStrategy launches OpenCode in HTTP server + SSE mode.
// The OpenCode CLI is started with --server --port <PORT>, exposing an HTTP
// API for sending messages and an SSE endpoint for receiving events.
type OpenCodeSDKExecutionStrategy struct{}

func NewOpenCodeSDKExecutionStrategy() *OpenCodeSDKExecutionStrategy {
	return &OpenCodeSDKExecutionStrategy{}
}

func (s *OpenCodeSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath := cmd.Command
	if !filepath.IsAbs(execPath) {
		resolved, err := exec.LookPath(execPath)
		if err != nil {
			return nil, fmt.Errorf("opencode-sdk: command not found: %s: %w", execPath, err)
		}
		execPath = resolved
	}
	if info, err := os.Stat(execPath); err != nil {
		return nil, fmt.Errorf("opencode-sdk: command not accessible: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("opencode-sdk: command is a directory: %s", execPath)
	}

	// Read the server port from the environment.
	portStr := cmd.Env["OPENCODE_SERVER_PORT"]
	if portStr == "" {
		return nil, fmt.Errorf("opencode-sdk: OPENCODE_SERVER_PORT not set in command env")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("opencode-sdk: invalid OPENCODE_SERVER_PORT: %s", portStr)
	}

	args := append([]string{}, cmd.Args...)
	c := exec.Command(execPath, args...)
	c.Dir = cmd.Cwd
	c.Env = buildSDKEnvList(cmd.Env)
	hideCommandWindow(c)

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("opencode-sdk: start: %w", err)
	}

	// Wait for the HTTP server to become ready.
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForOpenCodeReady(baseURL, 5*time.Second, 500*time.Millisecond); err != nil {
		_ = c.Process.Kill()
		_ = c.Wait()
		return nil, fmt.Errorf("opencode-sdk: server not ready: %w", err)
	}

	handle := &OpenCodeSDKExecutionHandle{
		cmd:      c,
		pid:      c.Process.Pid,
		baseURL:  baseURL,
		outputCh: make(chan []byte, 128),
		exitCh:   make(chan PTYExit, 1),
	}

	// Subscribe to SSE events.
	sseResp, err := http.Get(baseURL + "/events")
	if err != nil {
		_ = c.Process.Kill()
		_ = c.Wait()
		return nil, fmt.Errorf("opencode-sdk: failed to subscribe to SSE: %w", err)
	}
	handle.sseResp = sseResp

	go handle.readSSE()
	go handle.waitProcess()

	return handle, nil
}

// waitForOpenCodeReady polls the HTTP server until it responds or the
// timeout expires.
func waitForOpenCodeReady(baseURL string, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		if err != nil {
			lastErr = err
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timeout waiting for %s: %v", baseURL, lastErr)
}

// OpenCodeSDKExecutionHandle wraps an OpenCode server process communicating
// via HTTP API and SSE events.
type OpenCodeSDKExecutionHandle struct {
	cmd     *exec.Cmd
	pid     int
	baseURL string
	sseResp *http.Response

	outputCh chan []byte
	exitCh   chan PTYExit

	mu     sync.Mutex
	closed bool
}

func (h *OpenCodeSDKExecutionHandle) PID() int {
	return h.pid
}

// Write sends a user prompt to the OpenCode server via HTTP POST.
func (h *OpenCodeSDKExecutionHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("opencode session closed")
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	body, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return fmt.Errorf("opencode-sdk: marshal: %w", err)
	}

	resp, err := http.Post(h.baseURL+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("opencode-sdk: send message: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("opencode-sdk: message rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Interrupt sends an interrupt request to the OpenCode server via HTTP POST.
func (h *OpenCodeSDKExecutionHandle) Interrupt() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("opencode session closed")
	}

	resp, err := http.Post(h.baseURL+"/interrupt", "application/json", nil)
	if err != nil {
		return fmt.Errorf("opencode-sdk: interrupt: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (h *OpenCodeSDKExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *OpenCodeSDKExecutionHandle) Output() <-chan []byte {
	return h.outputCh
}

func (h *OpenCodeSDKExecutionHandle) Exit() <-chan PTYExit {
	return h.exitCh
}

func (h *OpenCodeSDKExecutionHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true

	// Close the SSE response body to stop the reader goroutine.
	if h.sseResp != nil && h.sseResp.Body != nil {
		_ = h.sseResp.Body.Close()
	}
	return nil
}

// readSSE reads SSE events from the OpenCode server and converts them
// to human-readable text on the output channel.
func (h *OpenCodeSDKExecutionHandle) readSSE() {
	defer close(h.outputCh)

	scanner := bufio.NewScanner(h.sseResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
			continue
		}

		// Empty line signals end of an SSE event.
		if line == "" && (eventType != "" || len(dataLines) > 0) {
			data := strings.TrimSpace(strings.Join(dataLines, "\n"))
			text := openCodeSSEToText(eventType, data)
			if text != "" {
				h.outputCh <- []byte(text + "\n")
			}
			eventType = ""
			dataLines = nil
			continue
		}
	}

	// Flush any remaining partial event.
	if eventType != "" || len(dataLines) > 0 {
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		text := openCodeSSEToText(eventType, data)
		if text != "" {
			h.outputCh <- []byte(text + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[opencode-sse-error] %v\n", err))
	}
}

// waitProcess waits for the OpenCode process to exit and reports via exitCh.
func (h *OpenCodeSDKExecutionHandle) waitProcess() {
	defer close(h.exitCh)

	err := h.cmd.Wait()
	var codePtr *int
	if h.cmd.ProcessState != nil {
		code := h.cmd.ProcessState.ExitCode()
		codePtr = &code
	}

	h.exitCh <- PTYExit{
		Code: codePtr,
		Err:  err,
	}

	_ = h.Close()
}

// openCodeSSEToText converts an SSE event from the OpenCode server into
// human-readable text for the output pipeline.
func openCodeSSEToText(eventType, data string) string {
	if data == "" {
		return ""
	}

	switch eventType {
	case "assistant", "message":
		// Try to extract text content from JSON payload.
		var payload struct {
			Content string `json:"content"`
			Text    string `json:"text"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if payload.Content != "" {
				return payload.Content
			}
			if payload.Text != "" {
				return payload.Text
			}
		}
		// If not JSON, treat as raw text.
		return data

	case "tool_call", "tool":
		var payload struct {
			ToolName string `json:"tool_name"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			name := payload.ToolName
			if name == "" {
				name = payload.Name
			}
			if name != "" {
				return fmt.Sprintf("⚡ %s", name)
			}
		}
		return ""

	case "complete", "done", "finish":
		var payload struct {
			Status  string `json:"status"`
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if payload.Summary != "" {
				return fmt.Sprintf("✓ %s: %s", payload.Status, payload.Summary)
			}
			if payload.Status != "" {
				return fmt.Sprintf("✓ %s", payload.Status)
			}
		}
		return ""

	case "error":
		return fmt.Sprintf("[opencode-error] %s", data)

	default:
		// For unknown event types, try to extract text content.
		if eventType == "" {
			return ""
		}
		var payload struct {
			Content string `json:"content"`
			Text    string `json:"text"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if payload.Content != "" {
				return payload.Content
			}
			if payload.Text != "" {
				return payload.Text
			}
		}
		return ""
	}
}
