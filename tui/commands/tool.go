package commands

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// RunTool 执行 tool 子命令。
func RunTool(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui tool <recommend>")
	}
	switch args[0] {
	case "recommend":
		return toolRecommend(args[1:])
	default:
		return NewUsageError("unknown tool action: %s", args[0])
	}
}

func toolRecommend(args []string) error {
	fs := flag.NewFlagSet("tool recommend", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	desc := strings.Join(fs.Args(), " ")
	if desc == "" {
		return NewUsageError("usage: maclaw-tui tool recommend <task description>")
	}

	// 检测已安装的工具
	knownTools := []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"}
	var installed []string
	for _, t := range knownTools {
		if _, err := exec.LookPath(t); err == nil {
			installed = append(installed, t)
		}
	}

	selector := tool.NewSelector()
	name, reason := selector.Recommend(desc, installed)

	if *jsonOut {
		return PrintJSON(map[string]string{
			"tool":      name,
			"reason":    reason,
			"installed": strings.Join(installed, ","),
		})
	}
	fmt.Printf("Recommended tool: %s\n", name)
	fmt.Printf("Reason: %s\n", reason)
	if len(installed) > 0 {
		fmt.Printf("Installed: %s\n", strings.Join(installed, ", "))
	} else {
		fmt.Println("Installed: (none detected)")
	}
	return nil
}
