package main

import (
	"fmt"
	"strings"
)

// GeminiAdapter launches the Gemini CLI (google-gemini/gemini-cli).
// Gemini CLI's headless mode (--output-format stream-json) is single-shot:
// it requires a prompt via -p or stdin, executes once, then exits.
// Unlike Claude Code, it does NOT support --input-format stream-json for
// persistent stdin-based multi-turn interaction.
// Therefore Gemini runs in PTY mode (interactive TUI) so the process stays
// alive and accepts user input continuously.
type GeminiAdapter struct {
	app *App
}

func NewGeminiAdapter(app *App) *GeminiAdapter {
	return &GeminiAdapter{app: app}
}

func (a *GeminiAdapter) ProviderName() string {
	return "gemini"
}

func (a *GeminiAdapter) ExecutionMode() ExecutionMode {
	return ExecModePTY
}

func (a *GeminiAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("gemini")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("gemini is not installed")
	}

	// Ensure Gemini CLI's first-run theme selection is pre-configured
	// so it doesn't block the remote PTY session with interactive prompts.
	if err := ensureGeminiOnboardingComplete(a.app); err != nil {
		if a.app != nil {
			a.app.log(fmt.Sprintf("[gemini-adapter] onboarding pre-check warning: %v", err))
		}
	}

	// In original (Google native) mode, don't inject model env or args
	// so Gemini CLI uses its own Google OAuth login and default settings.
	isOriginal := strings.ToLower(strings.TrimSpace(spec.ModelName)) == "original"

	extra := map[string]string{}
	if !isOriginal && spec.ModelID != "" {
		extra["GEMINI_MODEL"] = spec.ModelID
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	args := make([]string, 0, 8)
	if !isOriginal && spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--sandbox", "none")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"gemini.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
