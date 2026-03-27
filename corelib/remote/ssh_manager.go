package remote

import (
	"fmt"
	"sync"
	"time"
)

// SSHManagedSession 是 SSHSessionManager 管理的单个 SSH 会话。
type SSHManagedSession struct {
	mu           sync.Mutex
	ID           string
	Spec         SSHSessionSpec
	Status       SessionStatus
	Summary      SSHSessionSummary
	Handle       *SSHPTYSession
	PreviewLines []string
	CreatedAt    time.Time
	ExitCode     *int
	LastOutputAt time.Time
}

// PreviewTail 返回最后 n 行预览输出。
func (s *SSHManagedSession) PreviewTail(n int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || len(s.PreviewLines) == 0 {
		return nil
	}
	start := 0
	if len(s.PreviewLines) > n {
		start = len(s.PreviewLines) - n
	}
	out := make([]string, len(s.PreviewLines)-start)
	copy(out, s.PreviewLines[start:])
	return out
}

// LineCount 返回当前预览行数。
func (s *SSHManagedSession) LineCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.PreviewLines)
}

// NewLinesSince 返回从 afterLine 开始的新行。
func (s *SSHManagedSession) NewLinesSince(afterLine int) ([]string, SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines []string
	if len(s.PreviewLines) > afterLine {
		lines = make([]string, len(s.PreviewLines)-afterLine)
		copy(lines, s.PreviewLines[afterLine:])
	}
	return lines, s.Status
}

// GetSummary 返回会话摘要的副本。
func (s *SSHManagedSession) GetSummary() SSHSessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Summary
}

// SSHSessionManager 管理所有 SSH 远程会话的生命周期。
// 复用 SSHPool 做连接管理，对上层暴露与 TUISessionManager 一致的接口模式。
type SSHSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SSHManagedSession
	pool     *SSHPool
	onUpdate func(sessionID string)
	counter  int
}

// NewSSHSessionManager 创建 SSH 会话管理器。
func NewSSHSessionManager(pool *SSHPool) *SSHSessionManager {
	if pool == nil {
		pool = NewSSHPool()
	}
	return &SSHSessionManager{
		sessions: make(map[string]*SSHManagedSession),
		pool:     pool,
	}
}

// SetOnUpdate 设置会话状态变更回调。
func (m *SSHSessionManager) SetOnUpdate(fn func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUpdate = fn
}

// Pool 返回底层连接池（供外部查看连接状态）。
func (m *SSHSessionManager) Pool() *SSHPool { return m.pool }

// Create 创建并启动一个新的 SSH 交互会话。
func (m *SSHSessionManager) Create(spec SSHSessionSpec) (*SSHManagedSession, error) {
	spec.HostConfig.Defaults()
	hostID := spec.HostConfig.SSHHostID()

	// 从连接池获取连接
	client, err := m.pool.Acquire(spec.HostConfig)
	if err != nil {
		return nil, fmt.Errorf("acquire ssh connection: %w", err)
	}

	// 创建 PTY 会话
	ptySession := NewSSHPTYSession(client, hostID)
	if err := ptySession.Start(spec); err != nil {
		m.pool.Release(spec.HostConfig)
		return nil, fmt.Errorf("start ssh pty: %w", err)
	}

	now := time.Now()
	m.mu.Lock()
	m.counter++
	sessionID := spec.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("ssh_%s_%d", hostID, m.counter)
	}
	m.mu.Unlock()

	label := spec.HostConfig.Label
	if label == "" {
		label = hostID
	}

	session := &SSHManagedSession{
		ID:     sessionID,
		Spec:   spec,
		Status: SessionRunning,
		Summary: SSHSessionSummary{
			SessionID: sessionID,
			HostID:    hostID,
			HostLabel: label,
			Status:    string(SessionRunning),
			UpdatedAt: now.Unix(),
		},
		Handle:    ptySession,
		CreatedAt: now,
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	go m.runOutputLoop(session)
	go m.runExitLoop(session, spec.HostConfig)

	return session, nil
}

// Get 获取会话。
func (m *SSHSessionManager) Get(sessionID string) (*SSHManagedSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// List 列出所有 SSH 会话。
func (m *SSHSessionManager) List() []*SSHManagedSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*SSHManagedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// WriteInput 向 SSH 会话写入命令。
func (m *SSHSessionManager) WriteInput(sessionID, text string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Write([]byte(text + "\n"))
}

// Interrupt 向 SSH 会话发送 Ctrl+C。
func (m *SSHSessionManager) Interrupt(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Interrupt()
}

// Kill 终止 SSH 会话。
func (m *SSHSessionManager) Kill(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("ssh session %s not found", sessionID)
	}
	if s.Handle == nil {
		return fmt.Errorf("ssh session %s has no handle", sessionID)
	}
	return s.Handle.Kill()
}

// GetSessionStatus 实现 SessionProvider 接口。
func (m *SSHSessionManager) GetSessionStatus(sessionID string) (SessionStatus, bool) {
	s, ok := m.Get(sessionID)
	if !ok {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Status, true
}

// Close 关闭所有会话和连接池。
func (m *SSHSessionManager) Close() {
	m.mu.Lock()
	sessions := make([]*SSHManagedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*SSHManagedSession)
	m.mu.Unlock()

	for _, s := range sessions {
		if s.Handle != nil {
			_ = s.Handle.Close()
		}
	}
	m.pool.CloseAll()
}

func (m *SSHSessionManager) runOutputLoop(s *SSHManagedSession) {
	if s.Handle == nil {
		return
	}
	outCh := s.Handle.Output()
	if outCh == nil {
		return
	}
	for chunk := range outCh {
		if len(chunk) == 0 {
			continue
		}
		lines := splitSSHOutputLines(chunk)
		now := time.Now()

		s.mu.Lock()
		s.PreviewLines = append(s.PreviewLines, lines...)
		if len(s.PreviewLines) > 2000 {
			s.PreviewLines = s.PreviewLines[len(s.PreviewLines)-2000:]
		}
		s.LastOutputAt = now
		s.Summary.UpdatedAt = now.Unix()
		if len(lines) > 0 {
			s.Summary.LastOutput = lines[len(lines)-1]
		}
		s.mu.Unlock()

		m.mu.RLock()
		cb := m.onUpdate
		m.mu.RUnlock()
		if cb != nil {
			cb(s.ID)
		}
	}
}

func (m *SSHSessionManager) runExitLoop(s *SSHManagedSession, hostCfg SSHHostConfig) {
	if s.Handle == nil {
		return
	}
	exitCh := s.Handle.Exit()
	if exitCh == nil {
		return
	}
	exit := <-exitCh

	s.mu.Lock()
	s.Status = SessionExited
	s.Summary.Status = string(SessionExited)
	s.Summary.UpdatedAt = time.Now().Unix()
	if exit.Code != nil {
		s.ExitCode = exit.Code
	}
	if exit.Err != nil {
		s.Status = SessionError
		s.Summary.Status = string(SessionError)
	}
	s.mu.Unlock()

	_ = s.Handle.Close()
	m.pool.Release(hostCfg)

	m.mu.RLock()
	cb := m.onUpdate
	m.mu.RUnlock()
	if cb != nil {
		cb(s.ID)
	}
}

func splitSSHOutputLines(chunk []byte) []string {
	text := string(chunk)
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}
