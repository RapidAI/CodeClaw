package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// CodexSDKExecutionStrategy launches Codex in non-interactive SDK mode
// using `codex exec --json`.  Communication happens via structured JSONL
// on stdout.  User prompts are passed as trailing arguments to `codex exec`.
//
// Unlike Claude's bidirectional stream-json protocol, Codex exec is
// unidirectional: the initial prompt is given on the command line, and
// follow-up messages use `codex exec resume --last`.
type CodexSDKExecutionStrategy struct{}

func NewCodexSDKExecutionStrategy() *CodexSDKExecutionStrategy {
	return &CodexSDKExecutionStrategy{}
}

func (s *CodexSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath := cmd.Command
	if !filepath.IsAbs(execPath) {
		resolved, err := exec.LookPath(execPath)
		if err != nil {
			return nil, fmt.Errorf("codex-sdk: command not found: %s: %w", execPath, err)
		}
		execPath = resolved
	}
	if info, err := os.Stat(execPath); err != nil {
		return nil, fmt.Errorf("codex-sdk: command not accessible: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("codex-sdk: command is a directory: %s", execPath)
	}

	args := append([]string{}, cmd.Args...)
	c := exec.Command(execPath, args...)
	c.Dir = cmd.Cwd
	c.Env = buildSDKEnvList(cmd.Env)
	hideCommandWindow(c)

	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex-sdk: stdin pipe: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex-sdk: stdout pipe: %w", err)
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("codex-sdk: stderr pipe: %w", err)
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("codex-sdk: start: %w", err)
	}

	handle := &CodexSDKExecutionHandle{
		cmd:      c,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		pid:      c.Process.Pid,
		outputCh: make(chan []byte, 128),
		exitCh:   make(chan PTYExit, 1),
	}

	handle.readerWg.Add(2)
	go handle.readStdout()
	go handle.readStderr()
	go func() {
		handle.readerWg.Wait()
		close(handle.outputCh)
	}()
	go handle.waitProcess()

	return handle, nil
}

// CodexSDKExecutionHandle wraps a Codex process running in exec --json mode.
type CodexSDKExecutionHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	pid    int

	outputCh chan []byte
	exitCh   chan PTYExit

	readerWg sync.WaitGroup

	mu     sync.Mutex
	closed bool

	// threadID is the Codex thread/session ID reported in thread.started events.
	threadID string
}

func (h *CodexSDKExecutionHandle) PID() int {
	return h.pid
}

// Write sends a follow-up message to Codex.  Since `codex exec` is a
// one-shot command, follow-up messages spawn a new `codex exec resume --last`
// process.  For the initial prompt, the message is written to stdin which
// Codex reads as the task prompt.
func (h *CodexSDKExecutionHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("codex session closed")
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	// Write the prompt text to stdin followed by a newline.
	// For `codex exec`, stdin is read as the task prompt when no
	// trailing argument is provided, or can be used for piped input.
	_, err := h.stdin.Write(append([]byte(text), '\n'))
	return err
}

func (h *CodexSDKExecutionHandle) Interrupt() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *CodexSDKExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *CodexSDKExecutionHandle) Output() <-chan []byte {
	return h.outputCh
}

func (h *CodexSDKExecutionHandle) Exit() <-chan PTYExit {
	return h.exitCh
}

func (h *CodexSDKExecutionHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	_ = h.stdin.Close()
	return nil
}

// ThreadID returns the Codex thread ID if reported.
func (h *CodexSDKExecutionHandle) ThreadID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.threadID
}

func (h *CodexSDKExecutionHandle) readStdout() {
	defer h.readerWg.Done()

	scanner := bufio.NewScanner(h.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Try to parse as Codex JSONL event
		var event CodexEvent
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			// Not JSON — emit as raw output
			h.outputCh <- []byte(trimmed + "\n")
			continue
		}

		// Convert Codex event to human-readable text for the output pipeline
		text := codexEventToText(event)
		if text != "" {
			h.outputCh <- []byte(text + "\n")
		}

		// Track thread ID
		if event.Type == "thread.started" && event.ThreadID != "" {
			h.mu.Lock()
			h.threadID = event.ThreadID
			h.mu.Unlock()
		}
	}

	if err := scanner.Err(); err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[codex-read-error] %v\n", err))
	}
}

func (h *CodexSDKExecutionHandle) readStderr() {
	defer h.readerWg.Done()
	scanner := bufio.NewScanner(h.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			h.outputCh <- []byte("[stderr] " + line + "\n")
		}
	}
}

func (h *CodexSDKExecutionHandle) waitProcess() {
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
