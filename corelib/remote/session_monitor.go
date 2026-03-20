package remote

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// SessionProvider is the minimal interface needed by SessionMonitor to
// look up session status. Decouples from concrete RemoteSessionManager.
type SessionProvider interface {
	GetSessionStatus(sessionID string) (SessionStatus, bool)
}

// SessionMonitor periodically polls the status of busy coding sessions and
// pushes StatusEvents when the status changes.
type SessionMonitor struct {
	mu       sync.Mutex
	watches  map[string]*sessionWatch
	provider SessionProvider
	statusC  chan agent.StatusEvent
	interval time.Duration
}

type sessionWatch struct {
	sessionID  string
	loopID     string
	lastStatus SessionStatus
	cancelCh   chan struct{}
}

// NewSessionMonitor creates a SessionMonitor with the given polling interval.
func NewSessionMonitor(provider SessionProvider, statusC chan agent.StatusEvent, interval time.Duration) *SessionMonitor {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	return &SessionMonitor{
		watches:  make(map[string]*sessionWatch),
		provider: provider,
		statusC:  statusC,
		interval: interval,
	}
}

// StartWatching begins polling a session's status.
func (m *SessionMonitor) StartWatching(sessionID string, loopID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.watches[sessionID]; ok {
		m.stopWatchLocked(existing)
	}

	var initialStatus SessionStatus
	if m.provider != nil {
		if s, ok := m.provider.GetSessionStatus(sessionID); ok {
			initialStatus = s
		}
	}

	w := &sessionWatch{
		sessionID:  sessionID,
		loopID:     loopID,
		lastStatus: initialStatus,
		cancelCh:   make(chan struct{}),
	}
	m.watches[sessionID] = w
	go m.pollLoop(w)
}

// StopWatching stops polling for a session.
func (m *SessionMonitor) StopWatching(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.watches[sessionID]; ok {
		m.stopWatchLocked(w)
		delete(m.watches, sessionID)
	}
}

// IsWatching returns true if the session is currently being monitored.
func (m *SessionMonitor) IsWatching(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watches[sessionID]
	return ok
}

// WatchCount returns the number of sessions being monitored.
func (m *SessionMonitor) WatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.watches)
}

// Close stops all watches and releases resources.
func (m *SessionMonitor) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.watches {
		m.stopWatchLocked(w)
		delete(m.watches, id)
	}
}

func (m *SessionMonitor) stopWatchLocked(w *sessionWatch) {
	select {
	case <-w.cancelCh:
	default:
		close(w.cancelCh)
	}
}

func (m *SessionMonitor) pollLoop(w *sessionWatch) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[session-monitor-panic] session=%s recovered: %v\n", w.sessionID, r)
		}
	}()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.cancelCh:
			return
		case <-ticker.C:
			m.checkStatus(w)
		}
	}
}

func (m *SessionMonitor) checkStatus(w *sessionWatch) {
	if m.provider == nil {
		return
	}

	currentStatus, ok := m.provider.GetSessionStatus(w.sessionID)
	if !ok {
		m.emitEvent(agent.StatusEvent{
			Type:      agent.StatusEventSessionFailed,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   fmt.Sprintf("会话 %s 已被清理，监控停止", w.sessionID),
		})
		m.StopWatching(w.sessionID)
		return
	}

	if currentStatus == w.lastStatus {
		return
	}

	oldStatus := w.lastStatus
	w.lastStatus = currentStatus

	switch {
	case isBusyStatus(oldStatus) && currentStatus == SessionWaitingInput:
		m.emitEvent(agent.StatusEvent{
			Type:      agent.StatusEventSessionCompleted,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   fmt.Sprintf("编程会话 %s 已完成（等待输入）", w.sessionID),
		})

	case isBusyStatus(oldStatus) && (currentStatus == SessionExited || currentStatus == SessionError):
		evtType := agent.StatusEventSessionCompleted
		msg := fmt.Sprintf("编程会话 %s 已退出", w.sessionID)
		if currentStatus == SessionError {
			evtType = agent.StatusEventSessionFailed
			msg = fmt.Sprintf("编程会话 %s 出错退出", w.sessionID)
		}
		m.emitEvent(agent.StatusEvent{
			Type:      evtType,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   msg,
		})
		m.StopWatching(w.sessionID)

	case currentStatus == SessionExited || currentStatus == SessionError:
		m.emitEvent(agent.StatusEvent{
			Type:      agent.StatusEventStopped,
			LoopID:    w.loopID,
			SessionID: w.sessionID,
			Message:   fmt.Sprintf("会话 %s 状态变为 %s", w.sessionID, string(currentStatus)),
		})
		m.StopWatching(w.sessionID)
	}
}

func (m *SessionMonitor) emitEvent(evt agent.StatusEvent) {
	select {
	case m.statusC <- evt:
	default:
		fmt.Fprintf(os.Stderr, "[session-monitor] status channel full, dropping event for session=%s\n", evt.SessionID)
	}
}

func isBusyStatus(s SessionStatus) bool {
	return s == SessionBusy || s == SessionRunning
}
