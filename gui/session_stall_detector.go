package main

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// StallDetectorConfig holds configuration for the StallDetector.
type StallDetectorConfig struct {
	StallTimeout  time.Duration     // default 45s
	MaxNudgeCount int               // default 3
	NudgeMessages map[string]string // per-tool nudge text; key = tool name (lowercase)
	DefaultNudge  string            // default "continue"
}

// sessionStallState tracks stall monitoring state for a single session.
type sessionStallState struct {
	timer         *time.Timer
	stallState    StallState
	nudgeCount    int
	lastOutput    time.Time
	cancelCh      chan struct{}
	exec          ExecutionHandle
	tool          string
	lastNudgeText string // for echo filtering
}

// StallDetector manages stall detection for all active sessions.
type StallDetector struct {
	mu       sync.Mutex
	config   StallDetectorConfig
	sessions map[string]*sessionStallState
	logger   func(string)

	// OnStallStateChanged is called when a session's stall state changes.
	// Parameters: sessionID, new state, current nudge count.
	// Will be wired by the integration layer (Task 2.2 / 4.1).
	OnStallStateChanged func(sessionID string, state StallState, nudgeCount int)
}

// NewStallDetector creates a StallDetector with the given config.
// Zero-value fields are replaced with sensible defaults.
func NewStallDetector(config StallDetectorConfig, logger func(string)) *StallDetector {
	if config.StallTimeout <= 0 {
		config.StallTimeout = 45 * time.Second
	}
	if config.MaxNudgeCount <= 0 {
		config.MaxNudgeCount = 3
	}
	if config.DefaultNudge == "" {
		config.DefaultNudge = "continue"
	}
	if logger == nil {
		logger = func(string) {}
	}
	return &StallDetector{
		config:   config,
		sessions: make(map[string]*sessionStallState),
		logger:   logger,
	}
}

// StartMonitoring begins stall monitoring for the given session.
// If the tool is "codex" (case-insensitive), monitoring is skipped because
// Codex is one-shot and does not support interactive nudge.
// If the session is already being monitored, existing monitoring is stopped first.
func (d *StallDetector) StartMonitoring(sessionID string, exec ExecutionHandle, tool string) {
	if strings.EqualFold(tool, "codex") {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Stop existing monitoring for this session if any.
	if existing, ok := d.sessions[sessionID]; ok {
		d.stopSessionLocked(existing)
		delete(d.sessions, sessionID)
	}

	ss := &sessionStallState{
		timer:      time.NewTimer(d.config.StallTimeout),
		stallState: StallStateNormal,
		nudgeCount: 0,
		lastOutput: time.Now(),
		cancelCh:   make(chan struct{}),
		exec:       exec,
		tool:       tool,
	}
	d.sessions[sessionID] = ss

	go d.monitorLoop(sessionID, ss)
}

// StopMonitoring stops stall monitoring for the given session, releasing
// all timers and goroutines.
func (d *StallDetector) StopMonitoring(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ss, ok := d.sessions[sessionID]; ok {
		d.stopSessionLocked(ss)
		delete(d.sessions, sessionID)
	}
}

// ResetTimer resets the stall timer for the given session.
// If hasNewOutput is true and the session was in StallStateSuspected,
// the nudge counter is reset to 0 and the state returns to StallStateNormal.
func (d *StallDetector) ResetTimer(sessionID string, hasNewOutput bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ss, ok := d.sessions[sessionID]
	if !ok {
		return
	}

	// Reset the timer.
	if !ss.timer.Stop() {
		select {
		case <-ss.timer.C:
		default:
		}
	}
	ss.timer.Reset(d.config.StallTimeout)
	ss.lastOutput = time.Now()

	if hasNewOutput && ss.stallState == StallStateSuspected {
		ss.nudgeCount = 0
		ss.stallState = StallStateNormal
		if d.OnStallStateChanged != nil {
			d.OnStallStateChanged(sessionID, StallStateNormal, 0)
		}
	}
}

// GetState returns the current stall state for the given session.
// Returns StallStateNormal if the session is not being monitored.
func (d *StallDetector) GetState(sessionID string) StallState {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ss, ok := d.sessions[sessionID]; ok {
		return ss.stallState
	}
	return StallStateNormal
}

// GetNudgeCount returns the current nudge count for the given session.
// Returns 0 if the session is not being monitored.
func (d *StallDetector) GetNudgeCount(sessionID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if ss, ok := d.sessions[sessionID]; ok {
		return ss.nudgeCount
	}
	return 0
}

// IsNudgeEcho checks whether the given line matches the last nudge text
// sent to the session. This is a defensive measure for echo filtering.
func (d *StallDetector) IsNudgeEcho(sessionID string, line string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	ss, ok := d.sessions[sessionID]
	if !ok || ss.lastNudgeText == "" {
		return false
	}
	return strings.TrimSpace(line) == strings.TrimSpace(ss.lastNudgeText)
}

// Close stops all monitoring and releases resources.
func (d *StallDetector) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for id, ss := range d.sessions {
		d.stopSessionLocked(ss)
		delete(d.sessions, id)
	}
}

// stopSessionLocked stops monitoring for a single session.
// Caller must hold d.mu.
func (d *StallDetector) stopSessionLocked(ss *sessionStallState) {
	select {
	case <-ss.cancelCh:
		// already closed
	default:
		close(ss.cancelCh)
	}
	ss.timer.Stop()
}

// monitorLoop is the per-session goroutine that watches for stall timeouts
// and sends nudge messages.
func (d *StallDetector) monitorLoop(sessionID string, ss *sessionStallState) {
	defer func() {
		if r := recover(); r != nil {
			d.logger("[stall-detector-panic] session=" + sessionID + " recovered from panic")
		}
	}()

	for {
		select {
		case <-ss.cancelCh:
			return
		case <-ss.timer.C:
			d.mu.Lock()

			// Check if we've exceeded the max nudge count.
			if ss.nudgeCount >= d.config.MaxNudgeCount {
				ss.stallState = StallStateStuck
				cb := d.OnStallStateChanged
				nudgeCount := ss.nudgeCount
				d.mu.Unlock()

				if cb != nil {
					cb(sessionID, StallStateStuck, nudgeCount)
				}
				return
			}

			// Mark as suspected and increment nudge count.
			ss.stallState = StallStateSuspected
			ss.nudgeCount++
			cb := d.OnStallStateChanged
			nudgeCount := ss.nudgeCount
			d.mu.Unlock()

			if cb != nil {
				cb(sessionID, StallStateSuspected, nudgeCount)
			}

			// Determine nudge text.
			nudgeText := d.config.DefaultNudge
			d.mu.Lock()
			if msg, ok := d.config.NudgeMessages[strings.ToLower(ss.tool)]; ok {
				nudgeText = msg
			}
			ss.lastNudgeText = nudgeText
			exec := ss.exec
			d.mu.Unlock()

			// Send the nudge.
			if err := exec.Write([]byte(nudgeText)); err != nil {
				d.logger("[stall-nudge-error] session=" + sessionID + " error=" + err.Error())

				d.mu.Lock()
				ss.stallState = StallStateStuck
				cb2 := d.OnStallStateChanged
				nc := ss.nudgeCount
				d.mu.Unlock()

				if cb2 != nil {
					cb2(sessionID, StallStateStuck, nc)
				}
				return
			}

			d.logger("[stall-nudge] session=" + sessionID + " nudge_count=" + strconv.Itoa(nudgeCount))

			// Reset timer for the next stall timeout period.
			d.mu.Lock()
			ss.timer.Reset(d.config.StallTimeout)
			d.mu.Unlock()
		}
	}
}
