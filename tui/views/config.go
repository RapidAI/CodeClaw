package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfigEntry 配置项。
type ConfigEntry struct {
	Key   string
	Value string
	Desc  string
}

// ConfigModel 配置管理视图。
type ConfigModel struct {
	entries []ConfigEntry
	cursor  int
	editing bool
}

// NewConfigModel 创建配置视图。
func NewConfigModel() ConfigModel {
	return ConfigModel{
		entries: []ConfigEntry{
			{Key: "hub_url", Value: "", Desc: "Hub 服务器地址"},
			{Key: "token", Value: "", Desc: "认证令牌"},
			{Key: "data_dir", Value: "", Desc: "数据目录"},
			{Key: "max_iterations", Value: "12", Desc: "Agent 最大迭代次数"},
			{Key: "clawnet_enabled", Value: "false", Desc: "启用 ClawNet"},
		},
	}
}

// SetEntries 更新配置项。
func (m *ConfigModel) SetEntries(entries []ConfigEntry) {
	m.entries = entries
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
}

// Init 实现 tea.Model。
func (m ConfigModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

// View 渲染配置视图。
func (m ConfigModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(headerStyle.Render("  配置管理"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, e := range m.entries {
		val := e.Value
		if val == "" {
			val = dimStyle.Render("(未设置)")
		}
		line := fmt.Sprintf("  %-20s = %-20s  %s", e.Key, val, dimStyle.Render(e.Desc))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  ↑↓:选择  Enter:编辑  r:刷新")
	return b.String()
}
