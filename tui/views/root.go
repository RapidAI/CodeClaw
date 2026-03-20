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
	TabSchedule
	TabMemory
	TabAudit
	TabClawNet
	TabConfig
	TabChat
	TabCount
)

// TabNames 各 Tab 的显示名称。
var TabNames = [TabCount]string{"会话", "工具", "定时", "记忆", "审计", "ClawNet", "配置", "助手"}

// RootModel 是 TUI 的根 Model，管理 Tab 切换和子视图。
type RootModel struct {
	width  int
	height int
	tab    int

	// 子视图
	Sessions      SessionListModel
	SessionDetail *SessionDetailModel  // nil = 不显示详情
	SessionCreate *SessionCreateModel  // nil = 不显示创建表单
	Tools         ToolStatusModel
	Schedule      ScheduleModel
	Memory        MemoryModel
	Audit         AuditModel
	ClawNet       ClawNetModel
	Config        ConfigModel
	Chat          ChatModel
	StatusBar     StatusBarModel
	Help          HelpModel
}

// NewRootModel 创建根 Model。
func NewRootModel() RootModel {
	return RootModel{
		tab:       TabSessions,
		Sessions:  NewSessionListModel(),
		Tools:     NewToolStatusModel(),
		Schedule:  NewScheduleModel(),
		Memory:    NewMemoryModel(),
		Audit:     NewAuditModel(),
		ClawNet:   NewClawNetModel(),
		Config:    NewConfigModel(),
		Chat:      NewChatModel(),
		StatusBar: NewStatusBarModel(),
		Help:      NewHelpModel(),
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
		if m.SessionDetail != nil {
			d := *m.SessionDetail
			d, _ = d.Update(msg)
			m.SessionDetail = &d
		}
		if m.SessionCreate != nil {
			c := *m.SessionCreate
			c, _ = c.Update(msg)
			m.SessionCreate = &c
		}
	case SessionOpenMsg:
		detail := NewSessionDetailModel(msg.ID, msg.Tool, msg.Title)
		m.SessionDetail = &detail
		return m, nil
	case SessionCreateMsg:
		// 收集可用工具名称
		var toolNames []string
		for _, t := range m.Tools.tools {
			if t.Available {
				toolNames = append(toolNames, t.Name)
			}
		}
		if len(toolNames) == 0 {
			toolNames = []string{"(无可用工具)"}
		}
		create := NewSessionCreateModel(toolNames)
		m.SessionCreate = &create
		return m, nil
	case SessionCreateSubmitMsg:
		m.SessionCreate = nil
		m.StatusBar.SetMessage(fmt.Sprintf("创建会话: tool=%s project=%s", msg.Tool, msg.Project))
		return m, nil
	case tea.KeyMsg:
		// 帮助面板优先
		if m.Help.IsVisible() {
			var cmd tea.Cmd
			m.Help, cmd = m.Help.Update(msg)
			return m, cmd
		}
		// 创建会话表单
		if m.SessionCreate != nil {
			if msg.String() == "esc" {
				m.SessionCreate = nil
				return m, nil
			}
			var cmd tea.Cmd
			c := *m.SessionCreate
			c, cmd = c.Update(msg)
			m.SessionCreate = &c
			return m, cmd
		}
		// 会话详情模式下，Esc 返回列表
		if m.SessionDetail != nil {
			if msg.String() == "esc" {
				m.SessionDetail = nil
				return m, nil
			}
			var cmd tea.Cmd
			d := *m.SessionDetail
			d, cmd = d.Update(msg)
			m.SessionDetail = &d
			return m, cmd
		}
		// 编辑模式下不处理 Tab 切换
		if m.tab == TabConfig && m.Config.IsEditing() {
			break
		}
		if m.tab == TabAudit && m.Audit.IsFiltering() {
			break
		}
		if m.tab == TabChat && m.Chat.IsInputFocused() {
			break
		}
		switch msg.String() {
		case "?":
			m.Help.Toggle()
			return m, nil
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
	case TabSchedule:
		m.Schedule, cmd = m.Schedule.Update(msg)
	case TabMemory:
		m.Memory, cmd = m.Memory.Update(msg)
	case TabAudit:
		m.Audit, cmd = m.Audit.Update(msg)
	case TabClawNet:
		m.ClawNet, cmd = m.ClawNet.Update(msg)
	case TabConfig:
		m.Config, cmd = m.Config.Update(msg)
	case TabChat:
		m.Chat, cmd = m.Chat.Update(msg)
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

	// Overlay 优先级: Help > SessionCreate > SessionDetail > 正常 Tab
	if m.Help.IsVisible() {
		content = m.Help.View()
	} else if m.SessionCreate != nil {
		content = m.SessionCreate.View()
	} else if m.tab == TabSessions && m.SessionDetail != nil {
		content = m.SessionDetail.View()
	} else {
		switch m.tab {
		case TabSessions:
			content = m.Sessions.View()
		case TabTools:
			content = m.Tools.View()
		case TabSchedule:
			content = m.Schedule.View()
		case TabMemory:
			content = m.Memory.View()
		case TabAudit:
			content = m.Audit.View()
		case TabClawNet:
			content = m.ClawNet.View()
		case TabConfig:
			content = m.Config.View()
		case TabChat:
			content = m.Chat.View()
		}
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
