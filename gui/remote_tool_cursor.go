package main

import (
	"fmt"
)

// CursorAdapter launches the Cursor Agent CLI (`cursor-agent`).
// Cursor Agent supports `-p --output-format stream-json`, producing JSONL events
// compatible with Claude Code's stream-json protocol. It runs in SDK mode
// reusing the existing SDKExecutionStrategy.
type CursorAdapter struct {
	app *App
}

func NewCursorAdapter(app *App) *CursorAdapter {
	return &CursorAdapter{app: app}
}

func (a *CursorAdapter) ProviderName() string {
	return "cursor"
}

func (a *CursorAdapter) ExecutionMode() ExecutionMode {
	return ExecModeSDK
}

func (a *CursorAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("cursor")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("cursor agent is not installed")
	}

	env := buildOpenAICompatibleCommandEnv(spec.Env, nil)

	args := make([]string, 0, 8)
	// SDK mode: always include -p for Cursor Agent SDK mode and stream-json output
	args = append(args, "-p", "--output-format", "stream-json")
	if spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--yolo")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"cursor-agent.exe", "agent.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
	}, nil
}
