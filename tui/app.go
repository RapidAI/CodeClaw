package main

import (
	"context"
	"fmt"
	"os"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

// TUIApp 是 Bubble Tea 的顶层 Model，持有 Kernel 和 UI 状态。
type TUIApp struct {
	kernel *corelib.Kernel
	bridge *BubbleTeaEventBridge
	logger *TUILogger

	root  views.RootModel
	ready bool
	err   error
}

// kernelStartedMsg 内核启动完成的消息。
type kernelStartedMsg struct{ err error }

// kernelEventMsg 内核事件转发到 Bubble Tea 的消息。
type kernelEventMsg struct {
	eventType string
	data      interface{}
}

// NewTUIApp 创建 TUI 应用实例。
func NewTUIApp() *TUIApp {
	return &TUIApp{
		root: views.NewRootModel(),
	}
}

// Init 实现 tea.Model 接口。
func (a *TUIApp) Init() tea.Cmd {
	return a.initKernel
}

// initKernel 在后台初始化内核。
func (a *TUIApp) initKernel() tea.Msg {
	logger := NewTUILogger()
	a.logger = logger

	bridge := NewBubbleTeaEventBridge()
	a.bridge = bridge

	opts := buildKernelOptions(logger, bridge)
	kernel, err := corelib.NewKernel(opts)
	if err != nil {
		return kernelStartedMsg{err: err}
	}
	a.kernel = kernel

	// 在后台启动内核事件循环
	go func() {
		ctx := context.Background()
		if err := kernel.Run(ctx); err != nil {
			logger.Error("kernel run error: %v", err)
		}
	}()

	return kernelStartedMsg{}
}

// Update 实现 tea.Model 接口，处理消息。
func (a *TUIApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if a.kernel != nil {
				ctx := context.Background()
				_ = a.kernel.Shutdown(ctx)
			}
			return a, tea.Quit
		}

	case tea.WindowSizeMsg:
		a.ready = true

	case kernelStartedMsg:
		if msg.err != nil {
			a.err = msg.err
			a.root.StatusBar.SetMessage(fmt.Sprintf("内核初始化失败: %v", msg.err))
		} else {
			a.root.StatusBar.SetMessage("就绪")
			a.root.StatusBar.SetHubStatus("disconnected")
			a.root.Sessions.SetSessions(nil) // 清除 loading 状态
			a.root.Tools.SetTools(nil)
		}

	case kernelEventMsg:
		a.root.StatusBar.SetMessage(fmt.Sprintf("[%s] %v", msg.eventType, msg.data))
	}

	// 委托给 root model
	var cmd tea.Cmd
	a.root, cmd = a.root.Update(msg)
	return a, cmd
}

// View 实现 tea.Model 接口，渲染 UI。
func (a *TUIApp) View() string {
	if !a.ready {
		return "正在初始化 MaClaw TUI...\n"
	}
	if a.err != nil {
		return fmt.Sprintf("错误: %v\n\n按 q 退出\n", a.err)
	}
	return a.root.View()
}

// runTUI 启动 TUI 交互模式。
func runTUI() {
	app := NewTUIApp()
	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
