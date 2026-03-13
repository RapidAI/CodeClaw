package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type OpencodeAdapter struct {
	app *App
}

func NewOpencodeAdapter(app *App) *OpencodeAdapter {
	return &OpencodeAdapter{app: app}
}

func (a *OpencodeAdapter) ProviderName() string {
	return "opencode"
}

func (a *OpencodeAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	if spec.SessionID == "" {
		return CommandSpec{}, fmt.Errorf("opencode session id is required")
	}

	cfg, err := a.app.LoadConfig()
	if err != nil {
		return CommandSpec{}, err
	}
	if err := a.app.syncToOpencodeSettings(cfg, spec.ProjectPath, spec.SessionID); err != nil {
		return CommandSpec{}, err
	}

	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("opencode")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("opencode is not installed")
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

func (a *OpencodeAdapter) resolveExecutable(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		exePath := filepath.Join(filepath.Dir(cleaned), "opencode.exe")
		if _, err := os.Stat(exePath); err == nil {
			return exePath
		}
	}
	return cleaned
}

func (a *OpencodeAdapter) buildCommandEnv(base map[string]string, modelID string) map[string]string {
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

	if modelID != "" && env["OPENCODE_MODEL"] == "" {
		env["OPENCODE_MODEL"] = modelID
	}
	return env
}
