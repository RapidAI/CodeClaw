// Package views 包含 TUI 的所有视图组件。
package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Tab 索引常量。
const (
	TabSessions = iota
	TabTools
	TabConfig
	TabCount
)

// TabNames 各 Tab 的显示名称。
var TabNames = [TabCount]string{"会话", "工具", "配置"}

// RootModel 是 TUI 的根 Model，管理 Tab 切换和子视图。
type RootModel struct {
	width  int
	height int
	tab    int

	// 子视图
	Sessions  SessionListModel
	Tools     ToolStatusModel
	Config    ConfigModel
	StatusBar StatusBarModel
}

// NewRootModel 创建根 Model。
func NewRootModel() RootModel {
	return RootModel{
		tab:       TabSessions,
		Sessions:  NewSessionListModel(),
		Tools:     NewToolStatusModel(),
		Config:    NewConfigModel(),
		StatusBar: NewStatusBarModel(),
	}
}

// Init 实现 tea.Model。
func (m RootModel) Init() tea.Cmd {
	return nil
}

// Update 处理键盘导航和子视图更新。
func (m RootModel) Update(msg tea.Msg) (RootModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "right":
			m.tab = (m.tab + 1) % TabCount
			return m, nil
		case "shift+tab", "left":
			m.tab = (m.tab - 1 + TabCount) % TabCount
			return m, nil
		}
	}

	// 委托给当前活跃的子视图
	var cmd tea.Cmd
	switch m.tab {
	case TabSessions:
		m.Sessions, cmd = m.Sessions.Update(msg)
	case TabTools:
		m.Tools, cmd = m.Tools.Update(msg)
	case TabConfig:
		m.Config, cmd = m.Config.Update(msg)
	}

	// 状态栏始终更新
	var sbCmd tea.Cmd
	m.StatusBar, sbCmd = m.StatusBar.Update(msg)

	return m, tea.Batch(cmd, sbCmd)
}

// View 渲染完整 TUI 界面。
func (m RootModel) View() string {
	if m.width == 0 {
		return "正在初始化...\n"
	}

	// Tab 栏
	tabBar := m.renderTabs()

	// 内容区
	contentHeight := m.height - 4 // tab栏 + 状态栏 + 边距
	if contentHeight < 1 {
		contentHeight = 1
	}
	content := ""
	switch m.tab {
	case TabSessions:
		content = m.Sessions.View()
	case TabTools:
		content = m.Tools.View()
	case TabConfig:
		content = m.Config.View()
	}
	contentStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(contentHeight)
	content = contentStyle.Render(content)

	// 状态栏
	statusBar := m.StatusBar.View(m.width)

	return fmt.Sprintf("%s\n%s\n%s", tabBar, content, statusBar)
}

// renderTabs 渲染 Tab 栏。
func (m RootModel) renderTabs() string {
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Padding(0, 2)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Padding(0, 2)

	tabs := ""
	for i, name := range TabNames {
		if i == m.tab {
			tabs += activeStyle.Render(name)
		} else {
			tabs += inactiveStyle.Render(name)
		}
	}
	return tabs
}

// ActiveTab 返回当前活跃的 Tab 索引。
func (m RootModel) ActiveTab() int {
	return m.tab
}
