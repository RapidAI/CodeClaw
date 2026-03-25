package main

import (
	"fmt"
)

// GeminiAdapter launches the Gemini CLI (google-gemini/gemini-cli).
// Gemini CLI supports --experimental-acp which exposes a JSON-RPC based
// Agent Communication Protocol on stdin/stdout for structured bidirectional
// communication.  This is similar to Claude Code's --input-format stream-json
// but uses JSON-RPC instead of stream-json.
//
// The ACP protocol flow:
//  1. initialize → handshake with protocol version
//  2. session/new → create a new session
//  3. session/prompt → send user messages (streams session/update notifications)
//  4. session/cancel → interrupt current prompt
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
	return ExecModeGeminiACP
}

func (a *GeminiAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("gemini")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("gemini is not installed")
	}

	// NOTE: Gemini onboarding (theme, auth type, UI flags) is handled by
	// ensureToolOnboardingComplete in remote_session_manager.go before
	// BuildCommand is called.  No need to call it again here.

	// In original (Google native) mode, don't inject model env or args
	// so Gemini CLI uses its own Google OAuth login and default settings.
	isOriginal := spec.IsBuiltin

	env := buildOpenAICompatibleCommandEnv(spec.Env, map[string]string{})

	// ACP mode: use --experimental-acp for structured JSON-RPC communication
	args := []string{"--experimental-acp"}

	if !isOriginal && spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--yolo")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"gemini.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
	}, nil
}
