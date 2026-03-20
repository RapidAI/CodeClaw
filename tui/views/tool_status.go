package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ToolInfo 工具状态信息。
type ToolInfo struct {
	Name      string
	Available bool
	Version   string
	Path      string
}

// ToolStatusModel 工具状态视图。
type ToolStatusModel struct {
	tools   []ToolInfo
	cursor  int
	loading bool
}

// NewToolStatusModel 创建工具状态视图。
func NewToolStatusModel() ToolStatusModel {
	return ToolStatusModel{loading: true}
}

// SetTools 更新工具列表。
func (m *ToolStatusModel) SetTools(tools []ToolInfo) {
	m.tools = tools
	m.loading = false
}

// Init 实现 tea.Model。
func (m ToolStatusModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m ToolStatusModel) Update(msg tea.Msg) (ToolStatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tools)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

// View 渲染工具状态列表。
func (m ToolStatusModel) View() string {
	if m.loading {
		return "  正在检测工具状态..."
	}
	if len(m.tools) == 0 {
		return "  未检测到任何工具"
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-15s %-8s %-12s %s", "工具", "状态", "版本", "路径")))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, t := range m.tools {
		status := errStyle.Render("✗ 未安装")
		if t.Available {
			status = okStyle.Render("✓ 就绪 ")
		}
		line := fmt.Sprintf("  %-15s %s %-12s %s", t.Name, status, t.Version, t.Path)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  ↑↓:选择  r:刷新  Enter:启动")
	return b.String()
}
