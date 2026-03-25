package swarm

import (
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// SwarmSessionManager 抽象会话管理能力，由 GUI 和 TUI 各自实现。
type SwarmSessionManager interface {
	Create(spec SwarmLaunchSpec) (SwarmSession, error)
	Get(sessionID string) (SwarmSession, bool)
	Kill(sessionID string) error
	WriteInput(sessionID string, text string) error
}

// SwarmSession 抽象单个会话，屏蔽 GUI RemoteSession 和 TUI TUISession 的差异。
type SwarmSession interface {
	SessionID() string
	SessionStatus() SessionStatus
	SessionSummary() SwarmSessionSummary
	SessionOutput() string
}

// SwarmAppContext 抽象应用级能力（如已安装工具列表）。
type SwarmAppContext interface {
	ListInstalledTools() []InstalledToolInfo
}

// SwarmLLMCaller 抽象 LLM 调用能力。
type SwarmLLMCaller interface {
	CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error)
}

// SwarmLaunchSpec 编排器创建会话所需的参数，不依赖 GUI 或 TUI 特定类型。
type SwarmLaunchSpec struct {
	Tool         string
	ProjectPath  string
	Env          map[string]string
	LaunchSource string
}

// SwarmSessionSummary 编排器监控会话所需的摘要信息。
type SwarmSessionSummary struct {
	Status          string
	ProgressSummary string
	LastResult      string
	WaitingForUser  bool
	UpdatedAt       time.Time
}

// InstalledToolInfo 描述一个已安装的编程工具。
type InstalledToolInfo struct {
	Name     string
	CanStart bool
}

// SessionStatus 类型别名，复用 corelib/remote 的定义。
// 编排器代码通过此别名访问状态常量，无需直接导入 corelib/remote。
type SessionStatus = remote.SessionStatus

// 重新导出常用状态常量。
const (
	SessionStarting     = remote.SessionStarting
	SessionRunning      = remote.SessionRunning
	SessionBusy         = remote.SessionBusy
	SessionWaitingInput = remote.SessionWaitingInput
	SessionError        = remote.SessionError
	SessionExited       = remote.SessionExited
)
