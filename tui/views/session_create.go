package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionCreateSubmitMsg 提交创建会话请求。
type SessionCreateSubmitMsg struct {
	Tool    string
	Project string
}

// SessionCreateModel 创建会话的表单视图（overlay）。
type SessionCreateModel struct {
	tools       []string // 可用工具列表
	toolCursor  int
	projectInput textinput.Model
	focusField  int // 0=tool, 1=project
	width       int
}

// NewSessionCreateModel 创建会话创建表单。
func NewSessionCreateModel(tools []string) SessionCreateModel {
	ti := textinput.New()
	ti.Placeholder = "项目路径（可选）"
	ti.CharLimit = 256
	ti.Width = 40

	if len(tools) == 0 {
		tools = []string{"(无可用工具)"}
	}
	return SessionCreateModel{
		tools:        tools,
		projectInput: ti,
	}
}

// Init 实现 tea.Model。
func (m SessionCreateModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m SessionCreateModel) Update(msg tea.Msg) (SessionCreateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.focusField = (m.focusField + 1) % 2
			if m.focusField == 1 {
				m.projectInput.Focus()
				return m, textinput.Blink
			}
			m.projectInput.Blur()
			return m, nil
		case "enter":
			if m.focusField == 0 {
				// 从工具选择跳到项目路径
				m.focusField = 1
				m.projectInput.Focus()
				return m, textinput.Blink
			}
			// 提交
			tool := ""
			if m.toolCursor < len(m.tools) {
				tool = m.tools[m.toolCursor]
			}
			project := m.projectInput.Value()
			return m, func() tea.Msg {
				return SessionCreateSubmitMsg{Tool: tool, Project: project}
			}
		case "up", "k":
			if m.focusField == 0 && m.toolCursor > 0 {
				m.toolCursor--
			}
		case "down", "j":
			if m.focusField == 0 && m.toolCursor < len(m.tools)-1 {
				m.toolCursor++
			}
		}
	}

	// 项目路径输入框
	if m.focusField == 1 {
		var cmd tea.Cmd
		m.projectInput, cmd = m.projectInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View 渲染创建会话表单。
func (m SessionCreateModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	focusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	var b strings.Builder
	b.WriteString(titleStyle.Render("  ╭─ 新建会话 ─╮"))
	b.WriteString("\n\n")

	// 工具选择
	toolLabel := labelStyle.Render("  工具选择:")
	if m.focusField == 0 {
		toolLabel = focusStyle.Render("▸ 工具选择:")
	}
	b.WriteString(toolLabel + "\n")

	maxShow := 6
	start := 0
	if m.toolCursor >= maxShow {
		start = m.toolCursor - maxShow + 1
	}
	end := start + maxShow
	if end > len(m.tools) {
		end = len(m.tools)
	}
	for i := start; i < end; i++ {
		prefix := "    "
		if i == m.toolCursor {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("  ▸ %s", m.tools[i])))
		} else {
			b.WriteString(normalStyle.Render(fmt.Sprintf("%s%s", prefix, m.tools[i])))
		}
		b.WriteString("\n")
	}
	if len(m.tools) > maxShow {
		b.WriteString(dimStyle.Render(fmt.Sprintf("    ... 共 %d 个工具", len(m.tools))))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// 项目路径
	projLabel := labelStyle.Render("  项目路径:")
	if m.focusField == 1 {
		projLabel = focusStyle.Render("▸ 项目路径:")
	}
	b.WriteString(projLabel + "\n")
	b.WriteString("    " + m.projectInput.View() + "\n")

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Tab:切换字段  ↑↓:选工具  Enter:确认/下一步  Esc:取消"))
	return b.String()
}
