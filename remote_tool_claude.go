package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type ClaudeAdapter struct {
	app *App
}

func NewClaudeAdapter(app *App) *ClaudeAdapter {
	return &ClaudeAdapter{app: app}
}

func (a *ClaudeAdapter) ProviderName() string {
	return "claude"
}

func (a *ClaudeAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("claude")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("claude is not installed")
	}

	commandPath := a.resolveClaudeExecutable(status.Path)
	env := a.buildCommandEnv(spec.Env)

	args := make([]string, 0, 2)
	if spec.YoloMode {
		args = append(args, "--dangerously-skip-permissions")
	}

	return CommandSpec{
		Command: commandPath,
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}

func (a *ClaudeAdapter) resolveClaudeExecutable(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		exePath := filepath.Join(filepath.Dir(cleaned), "claude.exe")
		if _, err := os.Stat(exePath); err == nil {
			return exePath
		}
	}
	return cleaned
}

func (a *ClaudeAdapter) buildCommandEnv(base map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range base {
		env[k] = v
	}

	home, _ := os.UserHomeDir()
	localToolPath := filepath.Join(home, ".cceasy", "tools")
	npmPath := filepath.Join(os.Getenv("AppData"), "npm")
	nodePath := `C:\Program Files\nodejs`
	gitCmdPath := `C:\Program Files\Git\cmd`
	gitBinPath := `C:\Program Files\Git\bin`
	gitUsrBinPath := `C:\Program Files\Git\usr\bin`

	basePath := env["PATH"]
	if strings.TrimSpace(basePath) == "" {
		basePath = os.Getenv("PATH")
	}
	env["PATH"] = strings.Join([]string{
		localToolPath,
		npmPath,
		nodePath,
		gitCmdPath,
		gitBinPath,
		gitUsrBinPath,
		basePath,
	}, ";")

	if env["CLAUDE_CODE_USE_COLORS"] == "" {
		env["CLAUDE_CODE_USE_COLORS"] = "true"
	}
	if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] == "" {
		env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "64000"
	}

	return env
}
