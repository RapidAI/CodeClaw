package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	tea "github.com/charmbracelet/bubbletea"
)

// TUIToolLauncher 实现 corelib/tool.ToolLauncher 接口。
// 交互模式下使用 tea.ExecProcess 暂挂 TUI 并前台执行工具；
// headless 模式下直接 exec.Command 并输出到 stdout。
type TUIToolLauncher struct {
	program  *tea.Program
	headless bool
}

// NewTUIToolLauncher 创建 TUI 工具启动器。
func NewTUIToolLauncher(headless bool) *TUIToolLauncher {
	return &TUIToolLauncher{headless: headless}
}

// SetProgram 绑定 Bubble Tea Program（交互模式需要）。
func (l *TUIToolLauncher) SetProgram(p *tea.Program) {
	l.program = p
}

// Launch 实现 tool.ToolLauncher 接口。
func (l *TUIToolLauncher) Launch(ctx context.Context, opts tool.LaunchOptions) error {
	if opts.Mode == tool.LaunchHeadless || l.headless {
		return l.launchHeadless(ctx, opts)
	}
	return l.launchInteractive(ctx, opts)
}

// SupportsMode 实现 tool.ToolLauncher 接口。
func (l *TUIToolLauncher) SupportsMode(mode tool.ToolLaunchMode) bool {
	switch mode {
	case tool.LaunchInteractive:
		return !l.headless && l.program != nil
	case tool.LaunchHeadless:
		return true
	default:
		return false
	}
}

// launchInteractive 使用 tea.ExecProcess 暂挂 TUI 前台执行。
func (l *TUIToolLauncher) launchInteractive(_ context.Context, opts tool.LaunchOptions) error {
	if l.program == nil {
		return fmt.Errorf("no tea.Program bound, cannot launch interactive tool")
	}

	cmd := exec.Command(opts.Tool, opts.Args...)
	cmd.Dir = opts.ProjectDir
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if len(cmd.Env) > 0 {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}

	// tea.ExecProcess 会暂挂 TUI，将终端交给子进程
	cb := tea.ExecProcess(cmd, func(err error) tea.Msg {
		return toolFinishedMsg{name: opts.Tool, err: err}
	})
	l.program.Send(cb)
	return nil
}

// launchHeadless 直接执行命令并输出到 stdout/stderr。
func (l *TUIToolLauncher) launchHeadless(_ context.Context, opts tool.LaunchOptions) error {
	cmd := exec.Command(opts.Tool, opts.Args...)
	cmd.Dir = opts.ProjectDir
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if len(cmd.Env) > 0 {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// toolFinishedMsg 工具执行完成的消息。
type toolFinishedMsg struct {
	name string
	err  error
}
