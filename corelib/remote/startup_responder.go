package remote

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// StartupSessionWriter is the minimal interface needed to send keystrokes
// to a running session. Decouples from concrete RemoteSession/ExecutionHandle.
type StartupSessionWriter interface {
	Write(data []byte) error
}

// StartupAutoResponder watches the first N seconds of a PTY session's
// output for interactive startup prompts (theme selection, confirmation
// dialogs, etc.) and automatically sends the appropriate keystrokes.
type StartupAutoResponder struct {
	mu        sync.Mutex
	sessionID string
	writer    StartupSessionWriter
	logger    corelib.Logger
	startedAt time.Time
	window    time.Duration
	sentKeys  map[string]bool
	done      bool
	accum     strings.Builder
	accumLen  int
}

// NewStartupAutoResponder creates a new responder. Pass nil writer to
// create a disabled (no-op) responder.
func NewStartupAutoResponder(sessionID string, writer StartupSessionWriter, logger corelib.Logger, createdAt time.Time) *StartupAutoResponder {
	return &StartupAutoResponder{
		sessionID: sessionID,
		writer:    writer,
		logger:    logger,
		startedAt: createdAt,
		window:    30 * time.Second,
		sentKeys:  map[string]bool{},
		// Disabled by default — the onboarding pre-check should handle
		// startup prompts. Enable only if the pre-check is insufficient.
		done: true,
	}
}

// Feed is called with each batch of raw output lines. It checks for
// known startup prompts and sends the appropriate response.
func (r *StartupAutoResponder) Feed(rawLines []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.done {
		return
	}

	if time.Since(r.startedAt) > r.window {
		r.done = true
		return
	}

	for _, line := range rawLines {
		lower := strings.ToLower(line)
		if r.accumLen+len(lower)+1 <= 8192 {
			r.accum.WriteString(lower)
			r.accum.WriteString(" ")
			r.accumLen += len(lower) + 1
		}
	}

	accumulated := r.accum.String()

	for _, pattern := range StartupPatterns {
		if r.sentKeys[pattern.ID] {
			continue
		}
		if pattern.Match(accumulated) {
			r.sentKeys[pattern.ID] = true
			if r.logger != nil {
				r.logger.Info(fmt.Sprintf("[startup-responder] session=%s, matched=%q, sending=%q",
					r.sessionID, pattern.ID, pattern.Response))
			}
			go r.sendResponse(pattern.Response, pattern.Delay)
		}
	}

	if r.detectNormalMode(accumulated) {
		r.done = true
		if r.logger != nil {
			r.logger.Info(fmt.Sprintf("[startup-responder] session=%s, detected normal mode, stopping", r.sessionID))
		}
	}
}

func (r *StartupAutoResponder) sendResponse(keys string, delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}
	r.mu.Lock()
	w := r.writer
	r.mu.Unlock()
	if w == nil {
		return
	}
	for _, ch := range keys {
		if err := w.Write([]byte(string(ch))); err != nil {
			if r.logger != nil {
				r.logger.Warn(fmt.Sprintf("[startup-responder] write error session=%s: %v", r.sessionID, err))
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *StartupAutoResponder) detectNormalMode(accumulated string) bool {
	normalIndicators := []string{
		"type a message",
		"send a message",
		"what can i help",
		"how can i help",
		"what would you like",
		"enter your prompt",
		"claude >",
		"tips:",
	}
	for _, indicator := range normalIndicators {
		if strings.Contains(accumulated, indicator) {
			return true
		}
	}
	return false
}

// StartupPattern defines a pattern to match in startup output and the
// response to send.
type StartupPattern struct {
	ID       string
	Match    func(accumulated string) bool
	Response string        // keys to send (each char sent individually)
	Delay    time.Duration // wait before sending
}

// StartupPatterns is the list of known Claude Code startup prompts.
var StartupPatterns = []StartupPattern{
	{
		ID: "theme-selection",
		Match: func(acc string) bool {
			hasTheme := strings.Contains(acc, "theme") ||
				strings.Contains(acc, "color scheme") ||
				strings.Contains(acc, "appearance")
			hasNumbers := strings.Contains(acc, "1.") || strings.Contains(acc, "1)")
			return hasTheme && hasNumbers
		},
		Response: "1\r",
		Delay:    500 * time.Millisecond,
	},
	{
		ID: "numbered-menu",
		Match: func(acc string) bool {
			has1 := strings.Contains(acc, "1.") || strings.Contains(acc, "1)")
			has2 := strings.Contains(acc, "2.") || strings.Contains(acc, "2)")
			has3 := strings.Contains(acc, "3.") || strings.Contains(acc, "3)")
			isStartup := strings.Contains(acc, "select") ||
				strings.Contains(acc, "choose") ||
				strings.Contains(acc, "pick") ||
				strings.Contains(acc, "option") ||
				strings.Contains(acc, "preference")
			return has1 && has2 && has3 && isStartup
		},
		Response: "1\r",
		Delay:    500 * time.Millisecond,
	},
	{
		ID: "trust-project",
		Match: func(acc string) bool {
			return (strings.Contains(acc, "trust") && strings.Contains(acc, "project")) ||
				(strings.Contains(acc, "trust") && strings.Contains(acc, "folder"))
		},
		Response: "y\r",
		Delay:    500 * time.Millisecond,
	},
	{
		ID: "startup-confirm-yn",
		Match: func(acc string) bool {
			hasYN := strings.Contains(acc, "(y/n)") || strings.Contains(acc, "[y/n]")
			isStartup := strings.Contains(acc, "welcome") ||
				strings.Contains(acc, "setup") ||
				strings.Contains(acc, "first time") ||
				strings.Contains(acc, "getting started") ||
				strings.Contains(acc, "onboarding")
			return hasYN && isStartup
		},
		Response: "y\r",
		Delay:    500 * time.Millisecond,
	},
	{
		ID: "press-enter",
		Match: func(acc string) bool {
			return strings.Contains(acc, "press enter") ||
				strings.Contains(acc, "press any key") ||
				strings.Contains(acc, "hit enter")
		},
		Response: "\r",
		Delay:    300 * time.Millisecond,
	},
	{
		ID: "login-prompt",
		Match: func(acc string) bool {
			return (strings.Contains(acc, "log in") || strings.Contains(acc, "sign in") || strings.Contains(acc, "login")) &&
				(strings.Contains(acc, "anthropic") || strings.Contains(acc, "claude"))
		},
		Response: "\r",
		Delay:    500 * time.Millisecond,
	},
}
