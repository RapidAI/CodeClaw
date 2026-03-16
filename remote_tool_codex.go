package main

import (
	"fmt"
	"strings"
)

// CodexAdapter launches the OpenAI Codex CLI in non-interactive SDK mode
// using `codex exec --json --full-auto`.  This avoids PTY confirmation
// prompts entirely by leveraging Codex's structured JSONL output protocol.
//
// When YoloMode is enabled, `--full-auto` allows file edits and commands.
// Otherwise, the default read-only sandbox is used.
type CodexAdapter struct {
	app *App
}

func NewCodexAdapter(app *App) *CodexAdapter {
	return &CodexAdapter{app: app}
}

func (a *CodexAdapter) ProviderName() string {
	return "codex"
}

func (a *CodexAdapter) ExecutionMode() ExecutionMode {
	return ExecModeCodexSDK
}

func (a *CodexAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("codex")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("codex is not installed")
	}

	// In original (OpenAI native) mode, don't inject model or wire_api
	// so Codex uses its own `codex auth` login and default settings.
	isOriginal := strings.ToLower(strings.TrimSpace(spec.ModelName)) == "original"

	extra := map[string]string{}
	if !isOriginal {
		if spec.ModelID != "" {
			extra["OPENAI_MODEL"] = spec.ModelID
		}
		if spec.Env["WIRE_API"] == "" {
			extra["WIRE_API"] = "responses"
		}
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	// Use `codex exec` sub-command for non-interactive structured output.
	// --json streams JSONL events to stdout (thread.started, item.*, turn.*).
	// --full-auto allows file edits and command execution without prompts.
	args := []string{"exec", "--json"}

	if spec.YoloMode {
		args = append(args, "--full-auto")
	}

	if !isOriginal && spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"codex.exe", "openai.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
