package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type CodexAdapter struct {
	app *App
}

func NewCodexAdapter(app *App) *CodexAdapter {
	return &CodexAdapter{app: app}
}

func (a *CodexAdapter) ProviderName() string {
	return "codex"
}

func (a *CodexAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("codex")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("codex is not installed")
	}

	env := a.buildCommandEnv(spec)

	return CommandSpec{
		Command: a.resolveCodexExecutable(status.Path),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}

func (a *CodexAdapter) resolveCodexExecutable(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		for _, candidate := range []string{"codex.exe", "openai.exe"} {
			exePath := filepath.Join(filepath.Dir(cleaned), candidate)
			if _, err := os.Stat(exePath); err == nil {
				return exePath
			}
		}
	}
	return cleaned
}

func (a *CodexAdapter) buildCommandEnv(spec LaunchSpec) map[string]string {
	env := map[string]string{}
	for k, v := range spec.Env {
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

	if spec.ModelID != "" && env["OPENAI_MODEL"] == "" {
		env["OPENAI_MODEL"] = spec.ModelID
	}
	if env["WIRE_API"] == "" {
		env["WIRE_API"] = "responses"
	}

	return env
}
