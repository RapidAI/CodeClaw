package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ClawNet 子 Tab 常量。
const (
	ClawNetSubPeers = iota
	ClawNetSubTasks
	ClawNetSubStatus
	ClawNetSubCount
)

var clawNetSubNames = [ClawNetSubCount]string{"节点", "任务", "状态"}

// ClawNetPeerItem 节点列表项。
type ClawNetPeerItem struct {
	PeerID  string
	Addr    string
	Latency string
	Country string
}

// ClawNetTaskItem 任务列表项。
type ClawNetTaskItem struct {
	ID     string
	Status string
	Reward float64
	Title  string
}

// ClawNetStatusInfo 状态信息。
type ClawNetStatusInfo struct {
	PeerID   string
	Peers    int
	UnreadDM int
	Version  string
	Uptime   string
	Balance  float64
	Tier     string
	Energy   float64
}

// ClawNetModel ClawNet 视图。
type ClawNetModel struct {
	subTab  int
	peers   []ClawNetPeerItem
	tasks   []ClawNetTaskItem
	status  ClawNetStatusInfo
	cursor  int
	loading bool
}

// NewClawNetModel 创建 ClawNet 视图。
func NewClawNetModel() ClawNetModel {
	return ClawNetModel{loading: true}
}

// SetPeers 更新节点列表。
func (m *ClawNetModel) SetPeers(peers []ClawNetPeerItem) {
	m.peers = peers
	m.loading = false
}

// SetTasks 更新任务列表。
func (m *ClawNetModel) SetTasks(tasks []ClawNetTaskItem) {
	m.tasks = tasks
	m.loading = false
}

// SetStatus 更新状态信息。
func (m *ClawNetModel) SetStatus(status ClawNetStatusInfo) {
	m.status = status
	m.loading = false
}

// Init 实现 tea.Model。
func (m ClawNetModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m ClawNetModel) Update(msg tea.Msg) (ClawNetModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			m.subTab = ClawNetSubPeers
			m.cursor = 0
		case "2":
			m.subTab = ClawNetSubTasks
			m.cursor = 0
		case "3":
			m.subTab = ClawNetSubStatus
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			listLen := m.currentListLen()
			if m.cursor < listLen-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m ClawNetModel) currentListLen() int {
	switch m.subTab {
	case ClawNetSubPeers:
		return len(m.peers)
	case ClawNetSubTasks:
		return len(m.tasks)
	}
	return 0
}

// View 渲染 ClawNet 视图。
func (m ClawNetModel) View() string {
	if m.loading {
		return "  正在加载 ClawNet..."
	}

	// 子 Tab 栏
	subBar := m.renderSubTabs()

	var content string
	switch m.subTab {
	case ClawNetSubPeers:
		content = m.viewPeers()
	case ClawNetSubTasks:
		content = m.viewTasks()
	case ClawNetSubStatus:
		content = m.viewStatus()
	}

	return subBar + "\n" + content
}

func (m ClawNetModel) renderSubTabs() string {
	activeStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)

	tabs := "  "
	for i, name := range clawNetSubNames {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if i == m.subTab {
			tabs += activeStyle.Render(label)
		} else {
			tabs += inactiveStyle.Render(label)
		}
	}
	return tabs
}

func (m ClawNetModel) viewPeers() string {
	if len(m.peers) == 0 {
		return "  暂无连接节点"
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-20s %-20s %-10s %s", "PEER ID", "ADDR", "LATENCY", "COUNTRY")))
	b.WriteString("\n  " + strings.Repeat("─", 65) + "\n")

	for i, p := range m.peers {
		line := fmt.Sprintf("  %-20s %-20s %-10s %s",
			truncate(p.PeerID, 20), truncate(p.Addr, 20), p.Latency, p.Country)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("\n  共 %d 个节点  ↑↓:选择  r:刷新", len(m.peers)))
	return b.String()
}

func (m ClawNetModel) viewTasks() string {
	if len(m.tasks) == 0 {
		return "  暂无任务"
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-20s %-10s %-8s %s", "ID", "STATUS", "REWARD", "TITLE")))
	b.WriteString("\n  " + strings.Repeat("─", 65) + "\n")

	for i, t := range m.tasks {
		line := fmt.Sprintf("  %-20s %-10s %-8.1f %s",
			truncate(t.ID, 20), t.Status, t.Reward, truncate(t.Title, 30))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("\n  共 %d 个任务  ↑↓:选择  r:刷新", len(m.tasks)))
	return b.String()
}

func (m ClawNetModel) viewStatus() string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	s := m.status
	var b strings.Builder
	b.WriteString(labelStyle.Render("  ClawNet 状态"))
	b.WriteString("\n  " + strings.Repeat("─", 40) + "\n")
	b.WriteString(fmt.Sprintf("  PeerID:    %s\n", valStyle.Render(s.PeerID)))
	b.WriteString(fmt.Sprintf("  节点数:    %s\n", valStyle.Render(fmt.Sprintf("%d", s.Peers))))
	b.WriteString(fmt.Sprintf("  未读消息:  %s\n", valStyle.Render(fmt.Sprintf("%d", s.UnreadDM))))
	b.WriteString(fmt.Sprintf("  版本:      %s\n", valStyle.Render(s.Version)))
	b.WriteString(fmt.Sprintf("  运行时间:  %s\n", valStyle.Render(s.Uptime)))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("  积分信息"))
	b.WriteString("\n  " + strings.Repeat("─", 40) + "\n")
	b.WriteString(fmt.Sprintf("  余额:  %s\n", valStyle.Render(fmt.Sprintf("%.2f", s.Balance))))
	b.WriteString(fmt.Sprintf("  等级:  %s\n", valStyle.Render(s.Tier)))
	b.WriteString(fmt.Sprintf("  能量:  %s\n", valStyle.Render(fmt.Sprintf("%.2f", s.Energy))))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  r:刷新  1:节点  2:任务  3:状态"))
	return b.String()
}
