package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel 底部状态栏。
type StatusBarModel struct {
	hubStatus string // connected, disconnected, connecting
	message   string // 最近的日志/事件消息
}

// NewStatusBarModel 创建状态栏。
func NewStatusBarModel() StatusBarModel {
	return StatusBarModel{
		hubStatus: "disconnected",
		message:   "就绪",
	}
}

// SetHubStatus 更新 Hub 连接状态。
func (m *StatusBarModel) SetHubStatus(status string) {
	m.hubStatus = status
}

// SetMessage 更新状态消息。
func (m *StatusBarModel) SetMessage(msg string) {
	m.message = msg
}

// Init 实现 tea.Model。
func (m StatusBarModel) Init() tea.Cmd { return nil }

// Update 处理消息。
func (m StatusBarModel) Update(msg tea.Msg) (StatusBarModel, tea.Cmd) {
	return m, nil
}

// View 渲染状态栏（需要宽度参数）。
func (m StatusBarModel) View(width int) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Width(width)

	hubIcon := "○"
	hubColor := lipgloss.Color("196") // red
	switch m.hubStatus {
	case "connected":
		hubIcon = "●"
		hubColor = lipgloss.Color("82") // green
	case "connecting":
		hubIcon = "◌"
		hubColor = lipgloss.Color("226") // yellow
	}

	hubStyle := lipgloss.NewStyle().Foreground(hubColor)
	hub := hubStyle.Render(hubIcon) + " Hub"

	bar := fmt.Sprintf(" %s │ %s │ Tab:切换 q:退出", hub, m.message)
	return style.Render(bar)
}
