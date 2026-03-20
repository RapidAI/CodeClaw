package remote

import (
	"fmt"
	"time"
)

// BuildSessionInitEvent 创建会话初始化事件。
func BuildSessionInitEvent(sessionID, tool, projectPath string) ImportantEvent {
	return ImportantEvent{
		EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Type:      "session.init",
		Severity:  "info",
		Title:     "Session started",
		Summary:   fmt.Sprintf("Session started for %s in %s", tool, projectPath),
		Count:     1,
		CreatedAt: time.Now().Unix(),
	}
}

// BuildSessionFailedEvent 创建会话失败事件。
func BuildSessionFailedEvent(sessionID string, err error) ImportantEvent {
	summary := "Session failed before launch completed"
	if err != nil {
		summary = err.Error()
	}
	return ImportantEvent{
		EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Type:      "session.failed",
		Severity:  "error",
		Title:     "Session failed",
		Summary:   summary,
		Count:     1,
		CreatedAt: time.Now().Unix(),
	}
}

// BuildSessionClosedEvent 创建会话关闭事件。
func BuildSessionClosedEvent(sessionID string, exit PTYExit) ImportantEvent {
	severity := "info"
	title := "Session finished"
	summary := "Session exited successfully"
	if exit.Code != nil {
		summary = fmt.Sprintf("Session exited with code %d", *exit.Code)
		if *exit.Code != 0 {
			severity = "warn"
		}
	}
	if exit.Err != nil {
		severity = "error"
		title = "Session crashed"
		summary = exit.Err.Error()
	}
	return ImportantEvent{
		EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Type:      "session.closed",
		Severity:  severity,
		Title:     title,
		Summary:   summary,
		Count:     1,
		CreatedAt: time.Now().Unix(),
	}
}
