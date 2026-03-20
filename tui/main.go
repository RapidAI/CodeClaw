package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		// 默认启动 TUI 交互模式
		runTUI()
		return
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
	case "session":
		runSessionCommand(os.Args[2:])
	case "config":
		runConfigCommand(os.Args[2:])
	case "template":
		runLocalCommand("template", os.Args[2:])
	case "memory":
		runLocalCommand("memory", os.Args[2:])
	case "schedule":
		runLocalCommand("schedule", os.Args[2:])
	case "audit":
		runLocalCommand("audit", os.Args[2:])
	case "policy":
		runLocalCommand("policy", os.Args[2:])
	case "clawnet":
		if err := commands.RunClawNet(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "tool":
		if err := commands.RunTool(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "skill":
		if err := commands.RunSkill(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "--version", "-v":
		fmt.Printf("maclaw-tui %s\n", version)
	case "--help", "-h":
		printUsage()
	default:
		// 检查 --no-tui 标志
		for _, arg := range os.Args[1:] {
			if arg == "--no-tui" {
				runBatch()
				return
			}
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(commands.ExitUsage)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: maclaw-tui [command] [flags]

Commands:
  (default)     启动 TUI 交互界面
  daemon        以守护进程模式运行（无 UI，仅后台服务）
  session       会话管理（list/start/attach/kill）
  config        配置管理（get/set/export/import/schema）
  template      会话模板管理（list/create/delete）
  memory        记忆管理（list/search/save/delete/compress/backup）
  schedule      定时任务管理（list/create/delete/pause/resume/trigger）
  audit         审计日志查询（list）
  policy        安全策略查看（list）
  clawnet       ClawNet P2P 网络（status/peers/tasks/credits）
  tool          工具管理（recommend）
  skill         技能管理（list）

Flags:
  --no-tui      批处理模式（无交互 UI）
  --version     显示版本号
  --help        显示帮助信息
`)
}

// buildKernelOptions 从环境变量和命令行参数构建 KernelOptions。
func buildKernelOptions(logger corelib.Logger, emitter corelib.EventEmitter) corelib.KernelOptions {
	dataDir := os.Getenv("MACLAW_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = home + "/.maclaw"
	}

	return corelib.KernelOptions{
		DataDir:        dataDir,
		HubURL:         os.Getenv("MACLAW_HUB_URL"),
		HubToken:       os.Getenv("MACLAW_TOKEN"),
		MachineID:      os.Getenv("MACLAW_MACHINE_ID"),
		Logger:         logger,
		EventEmitter:   emitter,
		ClawNetEnabled: os.Getenv("MACLAW_CLAWNET") == "1",
	}
}

// runLocalCommand 处理本地数据子命令（template/memory/schedule/audit）。
func runLocalCommand(cmd string, args []string) {
	dataDir := commands.ResolveDataDir()
	var err error
	switch cmd {
	case "template":
		err = commands.RunTemplate(args, dataDir)
	case "memory":
		err = commands.RunMemory(args, dataDir)
	case "schedule":
		err = commands.RunSchedule(args, dataDir)
	case "audit":
		err = commands.RunAudit(args, dataDir)
	case "policy":
		err = commands.RunPolicy(args, dataDir)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

// runDaemon 以守护进程模式运行内核（无 UI）。
// 支持 --pid-file 和 --log-file 参数。
func runDaemon() {
	daemonFlags := flag.NewFlagSet("daemon", flag.ExitOnError)
	pidFile := daemonFlags.String("pid-file", "", "PID 文件路径")
	logFile := daemonFlags.String("log-file", "", "日志文件路径（默认 stderr）")
	daemonFlags.Parse(os.Args[2:])

	logger := NewTUILogger()
	if *logFile != "" {
		if err := logger.SetLogFile(*logFile); err != nil {
			fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
			os.Exit(1)
		}
		defer logger.Close()
	}

	logger.Info("maclaw-tui daemon starting (version %s)", version)

	// 写 PID 文件
	if *pidFile != "" {
		pid := fmt.Sprintf("%d", os.Getpid())
		if err := os.WriteFile(*pidFile, []byte(pid), 0644); err != nil {
			logger.Error("failed to write PID file: %v", err)
			os.Exit(1)
		}
		defer os.Remove(*pidFile)
		logger.Info("PID %s written to %s", pid, *pidFile)
	}

	opts := buildKernelOptions(logger, nil)
	kernel, err := corelib.NewKernel(opts)
	if err != nil {
		logger.Error("kernel init failed: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := kernel.Run(ctx); err != nil {
		logger.Error("kernel run error: %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5e9) // 5s
	defer shutdownCancel()
	_ = kernel.Shutdown(shutdownCtx)

	logger.Info("maclaw-tui daemon stopped")
}

// runBatch 批处理模式（--no-tui），执行一次性操作后退出。
func runBatch() {
	fmt.Fprintln(os.Stderr, "batch mode: not yet implemented")
	os.Exit(1)
}

// runSessionCommand 处理 session 子命令。
func runSessionCommand(args []string) {
	hubURL, token := resolveHubCredentials()
	if err := commands.RunSession(args, hubURL, token); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

// runConfigCommand 处理 config 子命令。
func runConfigCommand(args []string) {
	hubURL, token := resolveHubCredentials()
	if err := commands.RunConfig(args, hubURL, token); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

// exitCodeForError 根据错误类型返回退出码。
func exitCodeForError(err error) int {
	var ue *commands.UsageError
	if errors.As(err, &ue) {
		return commands.ExitUsage
	}
	return commands.ExitError
}

// resolveHubCredentials 从环境变量获取 Hub 连接信息。
func resolveHubCredentials() (hubURL, token string) {
	hubURL = os.Getenv("MACLAW_HUB_URL")
	if hubURL == "" {
		hubURL = "http://localhost:9099"
	}
	token = os.Getenv("MACLAW_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "Error: MACLAW_TOKEN environment variable is required")
		os.Exit(commands.ExitUsage)
	}
	return
}
