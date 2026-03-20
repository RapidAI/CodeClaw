package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModel 帮助面板（overlay）。
type HelpModel struct {
	visible bool
}

// NewHelpModel 创建帮助面板。
func NewHelpModel() HelpModel {
	return HelpModel{}
}

// Toggle 切换显示/隐藏。
func (m *HelpModel) Toggle() {
	m.visible = !m.visible
}

// IsVisible 返回是否可见。
func (m HelpModel) IsVisible() bool {
	return m.visible
}

// Init 实现 tea.Model。
func (m HelpModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "?" || msg.String() == "esc" {
			m.visible = false
		}
	}
	return m, nil
}

// View 渲染帮助面板。
func (m HelpModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(titleStyle.Render("  ╭─ 快捷键帮助 ─╮"))
	b.WriteString("\n\n")

	sections := []struct {
		name string
		keys []struct{ key, desc string }
	}{
		{"全局", []struct{ key, desc string }{
			{"Tab / →", "下一个标签页"},
			{"Shift+Tab / ←", "上一个标签页"},
			{"q", "退出"},
			{"Ctrl+C", "强制退出"},
			{"?", "显示/关闭帮助"},
		}},
		{"列表通用", []struct{ key, desc string }{
			{"↑ / k", "上移"},
			{"↓ / j", "下移"},
			{"g", "跳到顶部"},
			{"G", "跳到底部"},
			{"r", "刷新"},
		}},
		{"会话", []struct{ key, desc string }{
			{"Enter", "查看详情"},
			{"n / c", "新建会话"},
			{"d / x", "终止会话"},
		}},
		{"定时任务", []struct{ key, desc string }{
			{"p", "暂停/恢复"},
			{"d", "删除"},
		}},
		{"记忆", []struct{ key, desc string }{
			{"d", "删除"},
		}},
		{"配置", []struct{ key, desc string }{
			{"Enter", "编辑"},
			{"Esc", "取消编辑"},
		}},
		{"ClawNet", []struct{ key, desc string }{
			{"1/2/3", "切换子标签"},
		}},
		{"会话详情", []struct{ key, desc string }{
			{"↑↓", "滚动"},
			{"g/G", "首/尾"},
			{"Esc", "返回列表"},
		}},
		{"AI 助手", []struct{ key, desc string }{
			{"i", "开始输入"},
			{"Enter", "发送消息"},
			{"Esc", "退出输入"},
			{"c", "清除历史"},
			{"↑↓", "滚动消息"},
		}},
	}

	for _, sec := range sections {
		b.WriteString(sectionStyle.Render("  " + sec.name))
		b.WriteString("\n")
		for _, kv := range sec.keys {
			b.WriteString("    ")
			b.WriteString(keyStyle.Render(fmt.Sprintf("%-16s", kv.key)))
			b.WriteString(descStyle.Render(kv.desc))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render("  按 ? 或 Esc 关闭"))
	return b.String()
}
