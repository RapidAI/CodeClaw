package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type IFlowAdapter struct {
	app *App
}

func NewIFlowAdapter(app *App) *IFlowAdapter {
	return &IFlowAdapter{app: app}
}

func (a *IFlowAdapter) ProviderName() string {
	return "iflow"
}

func (a *IFlowAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	if spec.SessionID == "" {
		return CommandSpec{}, fmt.Errorf("iflow session id is required")
	}

	cfg, err := a.app.LoadConfig()
	if err != nil {
		return CommandSpec{}, err
	}
	if err := a.app.syncToIFlowSettings(cfg, spec.ProjectPath, spec.SessionID); err != nil {
		return CommandSpec{}, err
	}

	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("iflow")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("iflow is not installed")
	}

	env := a.buildCommandEnv(spec.Env, spec.ModelID)
	return CommandSpec{
		Command: a.resolveExecutable(status.Path),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}

func (a *IFlowAdapter) resolveExecutable(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		exePath := filepath.Join(filepath.Dir(cleaned), "iflow.exe")
		if _, err := os.Stat(exePath); err == nil {
			return exePath
		}
	}
	return cleaned
}

func (a *IFlowAdapter) buildCommandEnv(base map[string]string, modelID string) map[string]string {
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

	if modelID != "" && env["IFLOW_MODEL"] == "" {
		env["IFLOW_MODEL"] = modelID
	}
	return env
}
