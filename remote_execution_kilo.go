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

// KiloSDKExecutionStrategy launches Kilo in HTTP server + SSE mode.
// Kilo is an OpenCode fork started with `kilo serve --port <PORT>`, exposing
// an HTTP API for sending messages and an SSE endpoint for receiving events.
type KiloSDKExecutionStrategy struct{}

func NewKiloSDKExecutionStrategy() *KiloSDKExecutionStrategy {
	return &KiloSDKExecutionStrategy{}
}

func (s *KiloSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath := cmd.Command
	if !filepath.IsAbs(execPath) {
		resolved, err := exec.LookPath(execPath)
		if err != nil {
			return nil, fmt.Errorf("kilo-sdk: command not found: %s: %w", execPath, err)
		}
		execPath = resolved
	}
	if info, err := os.Stat(execPath); err != nil {
		return nil, fmt.Errorf("kilo-sdk: command not accessible: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("kilo-sdk: command is a directory: %s", execPath)
	}

	// Read the server port from the environment.
	portStr := cmd.Env["KILO_SERVER_PORT"]
	if portStr == "" {
		return nil, fmt.Errorf("kilo-sdk: KILO_SERVER_PORT not set in command env")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("kilo-sdk: invalid KILO_SERVER_PORT: %s", portStr)
	}

	args := append([]string{}, cmd.Args...)
	c := exec.Command(execPath, args...)
	c.Dir = cmd.Cwd
	c.Env = buildSDKEnvList(cmd.Env)
	hideCommandWindow(c)

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("kilo-sdk: start: %w", err)
	}

	// Wait for the HTTP server to become ready.
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForOpenCodeReady(baseURL, 5*time.Second, 500*time.Millisecond); err != nil {
		_ = c.Process.Kill()
		_ = c.Wait()
		return nil, fmt.Errorf("kilo-sdk: server not ready: %w", err)
	}

	handle := &KiloSDKExecutionHandle{
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
		return nil, fmt.Errorf("kilo-sdk: failed to subscribe to SSE: %w", err)
	}
	handle.sseResp = sseResp

	go handle.readSSE()
	go handle.waitProcess()

	return handle, nil
}

// KiloSDKExecutionHandle wraps a Kilo server process communicating
// via HTTP API and SSE events.
type KiloSDKExecutionHandle struct {
	cmd     *exec.Cmd
	pid     int
	baseURL string
	sseResp *http.Response

	outputCh chan []byte
	exitCh   chan PTYExit

	mu     sync.Mutex
	closed bool
}

func (h *KiloSDKExecutionHandle) PID() int {
	return h.pid
}

// Write sends a user prompt to the Kilo server via HTTP POST.
func (h *KiloSDKExecutionHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("kilo session closed")
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	body, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return fmt.Errorf("kilo-sdk: marshal: %w", err)
	}

	resp, err := http.Post(h.baseURL+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("kilo-sdk: send message: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("kilo-sdk: message rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Interrupt sends an interrupt request to the Kilo server via HTTP POST.
func (h *KiloSDKExecutionHandle) Interrupt() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("kilo session closed")
	}

	resp, err := http.Post(h.baseURL+"/interrupt", "application/json", nil)
	if err != nil {
		return fmt.Errorf("kilo-sdk: interrupt: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (h *KiloSDKExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *KiloSDKExecutionHandle) Output() <-chan []byte {
	return h.outputCh
}

func (h *KiloSDKExecutionHandle) Exit() <-chan PTYExit {
	return h.exitCh
}

func (h *KiloSDKExecutionHandle) Close() error {
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

// readSSE reads SSE events from the Kilo server and converts them
// to human-readable text on the output channel. Reuses openCodeSSEToText
// since Kilo (an OpenCode fork) uses the same SSE protocol.
func (h *KiloSDKExecutionHandle) readSSE() {
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
		h.outputCh <- []byte(fmt.Sprintf("[kilo-sse-error] %v\n", err))
	}
}

// waitProcess waits for the Kilo process to exit and reports via exitCh.
func (h *KiloSDKExecutionHandle) waitProcess() {
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
