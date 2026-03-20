package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AuditItem 审计日志列表项。
type AuditItem struct {
	Time     string // "2006-01-02 15:04"
	Tool     string
	Risk     string // low, medium, high, critical
	Policy   string // allow, deny, ask, audit
	Result   string
}

// AuditRefreshMsg 请求刷新审计日志（带过滤条件）。
type AuditRefreshMsg struct {
	ToolFilter string
	RiskFilter string
}

// AuditModel 审计日志视图。
type AuditModel struct {
	entries    []AuditItem
	filtered  []AuditItem
	cursor    int
	loading   bool
	filtering bool
	filterInput textinput.Model
	filterText  string
}

// NewAuditModel 创建审计日志视图。
func NewAuditModel() AuditModel {
	ti := textinput.New()
	ti.Placeholder = "输入工具名或风险等级过滤..."
	ti.CharLimit = 64
	ti.Width = 40
	return AuditModel{loading: true, filterInput: ti}
}

// SetEntries 更新审计日志列表。
func (m *AuditModel) SetEntries(entries []AuditItem) {
	m.entries = entries
	m.loading = false
	m.applyFilter()
}

// IsFiltering 返回是否处于过滤输入模式。
func (m AuditModel) IsFiltering() bool {
	return m.filtering
}

func (m *AuditModel) applyFilter() {
	if m.filterText == "" {
		m.filtered = m.entries
	} else {
		kw := strings.ToLower(m.filterText)
		m.filtered = nil
		for _, e := range m.entries {
			if strings.Contains(strings.ToLower(e.Tool), kw) ||
				strings.Contains(strings.ToLower(e.Risk), kw) ||
				strings.Contains(strings.ToLower(e.Policy), kw) ||
				strings.Contains(strings.ToLower(e.Result), kw) {
				m.filtered = append(m.filtered, e)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

// Init 实现 tea.Model。
func (m AuditModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m AuditModel) Update(msg tea.Msg) (AuditModel, tea.Cmd) {
	if m.filtering {
		return m.updateFiltering(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.filtered) > 0 {
				m.cursor = len(m.filtered) - 1
			}
		case "/":
			m.filtering = true
			m.filterInput.SetValue(m.filterText)
			m.filterInput.Focus()
			m.filterInput.CursorEnd()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m AuditModel) updateFiltering(msg tea.Msg) (AuditModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			m.filterText = m.filterInput.Value()
			m.filtering = false
			m.filterInput.Blur()
			m.applyFilter()
			return m, nil
		case "esc":
			m.filtering = false
			m.filterInput.Blur()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

func riskColor(risk string) lipgloss.Color {
	switch risk {
	case "low":
		return lipgloss.Color("82")
	case "medium":
		return lipgloss.Color("226")
	case "high":
		return lipgloss.Color("208")
	case "critical":
		return lipgloss.Color("196")
	default:
		return lipgloss.Color("252")
	}
}

// View 渲染审计日志列表。
func (m AuditModel) View() string {
	if m.loading {
		return "  正在加载审计日志..."
	}
	if len(m.entries) == 0 {
		return "  暂无审计日志\n\n  使用 CLI 查询: maclaw-tui audit list"
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	var b strings.Builder

	// 过滤栏
	if m.filtering {
		b.WriteString(filterStyle.Render("  过滤: "))
		b.WriteString(m.filterInput.View())
		b.WriteString("\n")
	} else if m.filterText != "" {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  过滤: %s (/:修改  Esc:清除)", m.filterText)))
		b.WriteString("\n")
	}

	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-18s %-18s %-10s %-8s %s", "TIME", "TOOL", "RISK", "POLICY", "RESULT")))
	b.WriteString("\n  " + strings.Repeat("─", 72) + "\n")

	for i, e := range m.filtered {
		if i == m.cursor {
			plainLine := fmt.Sprintf("  %-18s %-18s %-10s %-8s %s",
				e.Time, truncate(e.Tool, 18), e.Risk, e.Policy, truncate(e.Result, 20))
			b.WriteString(selectedStyle.Render(plainLine))
		} else {
			riskStyled := lipgloss.NewStyle().Foreground(riskColor(e.Risk)).Render(fmt.Sprintf("%-10s", e.Risk))
			b.WriteString(normalStyle.Render(fmt.Sprintf("  %-18s %-18s ", e.Time, truncate(e.Tool, 18))))
			b.WriteString(riskStyled)
			b.WriteString(normalStyle.Render(fmt.Sprintf(" %-8s %s", e.Policy, truncate(e.Result, 20))))
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("\n  共 %d/%d 条  ↑↓:选择  g/G:首/尾  /:过滤  r:刷新", len(m.filtered), len(m.entries)))
	return b.String()
}
