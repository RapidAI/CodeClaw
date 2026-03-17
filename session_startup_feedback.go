package main

import (
	"fmt"
	"time"
)

// SessionStartupFeedback monitors session startup progress and pushes
// status updates to the caller via a ProgressCallback.
type SessionStartupFeedback struct {
	manager *RemoteSessionManager
}

// NewSessionStartupFeedback creates a new SessionStartupFeedback instance.
func NewSessionStartupFeedback(manager *RemoteSessionManager) *SessionStartupFeedback {
	return &SessionStartupFeedback{manager: manager}
}

// WatchStartup monitors the startup of a session in a background goroutine.
// Every 3 seconds it checks the session status and pushes a progress message.
// When the session reaches "running" status, a success notification is sent.
// After 60 seconds without reaching "running", a timeout warning is sent.
func (f *SessionStartupFeedback) WatchStartup(sessionID string, callback ProgressCallback) {
	go f.watchLoop(sessionID, callback)
}

func (f *SessionStartupFeedback) watchLoop(sessionID string, callback ProgressCallback) {
	messages := []string{
		"正在初始化工具",
		"正在加载项目",
		"等待工具就绪",
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()

	msgIdx := 0

	for {
		select {
		case <-ticker.C:
			session, ok := f.manager.Get(sessionID)
			if !ok {
				callback("⚠️ 会话未找到: " + sessionID)
				return
			}

			session.mu.RLock()
			status := session.Status
			session.mu.RUnlock()

			if status == SessionRunning {
				callback(fmt.Sprintf("✅ 会话已就绪 (ID: %s, 工具: %s)", session.ID, session.Tool))
				return
			}

			callback(messages[msgIdx%len(messages)])
			msgIdx++

		case <-timer.C:
			callback("⚠️ 会话启动超时（已等待 60 秒），请检查日志或重试")
			return
		}
	}
}
