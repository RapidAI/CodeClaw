package main

// swarm_adapters.go — TUI adapter implementations for corelib/swarm interfaces.
// These adapters bridge TUI-specific types (TUISessionManager, etc.)
// to the abstract interfaces expected by swarm.SwarmOrchestrator.

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ---------------------------------------------------------------------------
// SwarmSessionManager adapter
// ---------------------------------------------------------------------------

// tuiSessionAdapter adapts TUISessionManager to swarm.SwarmSessionManager.
type tuiSessionAdapter struct {
	manager *TUISessionManager
}

func (a *tuiSessionAdapter) Create(spec swarm.SwarmLaunchSpec) (swarm.SwarmSession, error) {
	ls := remote.LaunchSpec{
		Tool:        spec.Tool,
		ProjectPath: spec.ProjectPath,
		Env:         spec.Env,
	}
	session, err := a.manager.Create(ls)
	if err != nil {
		return nil, err
	}
	return &tuiSessionWrapper{session: session}, nil
}

func (a *tuiSessionAdapter) Get(sessionID string) (swarm.SwarmSession, bool) {
	s, ok := a.manager.Get(sessionID)
	if !ok {
		return nil, false
	}
	return &tuiSessionWrapper{session: s}, true
}

func (a *tuiSessionAdapter) Kill(sessionID string) error {
	return a.manager.Kill(sessionID)
}

func (a *tuiSessionAdapter) WriteInput(sessionID, text string) error {
	return a.manager.WriteInput(sessionID, text)
}

// ---------------------------------------------------------------------------
// SwarmSession adapter
// ---------------------------------------------------------------------------

// tuiSessionWrapper adapts TUISession to swarm.SwarmSession.
type tuiSessionWrapper struct {
	session *TUISession
}

func (w *tuiSessionWrapper) SessionID() string {
	return w.session.ID
}

func (w *tuiSessionWrapper) SessionStatus() swarm.SessionStatus {
	w.session.mu.Lock()
	defer w.session.mu.Unlock()
	return swarm.SessionStatus(w.session.Status)
}

func (w *tuiSessionWrapper) SessionSummary() swarm.SwarmSessionSummary {
	w.session.mu.Lock()
	defer w.session.mu.Unlock()
	return swarm.SwarmSessionSummary{
		Status:          string(w.session.Status),
		ProgressSummary: w.session.Summary.ProgressSummary,
		LastResult:      w.session.Summary.LastResult,
	}
}

func (w *tuiSessionWrapper) SessionOutput() string {
	w.session.mu.Lock()
	defer w.session.mu.Unlock()
	return w.session.Summary.LastResult
}

// ---------------------------------------------------------------------------
// SwarmAppContext adapter
// ---------------------------------------------------------------------------

// tuiAppContext adapts TUI tool detection to swarm.SwarmAppContext.
type tuiAppContext struct{}

func (c *tuiAppContext) ListInstalledTools() []swarm.InstalledToolInfo {
	detected := commands.DetectTools()
	var result []swarm.InstalledToolInfo
	for _, dt := range detected {
		if dt.Available {
			result = append(result, swarm.InstalledToolInfo{
				Name:     dt.Name,
				CanStart: true,
			})
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// SwarmLLMCaller adapter
// ---------------------------------------------------------------------------

// tuiLLMCaller adapts TUI LLM config to swarm.SwarmLLMCaller.
type tuiLLMCaller struct {
	cfg corelib.MaclawLLMConfig
}

func (c *tuiLLMCaller) CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error) {
	if c.cfg.URL == "" {
		return nil, fmt.Errorf("LLM not configured")
	}
	messages := []interface{}{
		map[string]string{"role": "user", "content": prompt},
	}
	client := &http.Client{Timeout: timeout}
	resp, err := agent.DoSimpleLLMRequest(c.cfg, messages, client, timeout)
	if err != nil {
		return nil, err
	}
	return []byte(resp.Content), nil
}

// ---------------------------------------------------------------------------
// Notifier adapter
// ---------------------------------------------------------------------------

// tuiNotifier implements swarm.Notifier for terminal output.
type tuiNotifier struct{}

func (n *tuiNotifier) NotifyPhaseChange(run *swarm.SwarmRun, phase swarm.SwarmPhase) error {
	log.Printf("[Swarm %s] Phase → %s", run.ID, phase)
	return nil
}

func (n *tuiNotifier) NotifyAgentComplete(run *swarm.SwarmRun, ag *swarm.SwarmAgent) error {
	log.Printf("[Swarm %s] Agent %s (%s) completed task %d", run.ID, ag.ID, ag.Role, ag.TaskIndex)
	return nil
}

func (n *tuiNotifier) NotifyFailure(run *swarm.SwarmRun, failType string, summary string) error {
	log.Printf("[Swarm %s] %s failure: %s", run.ID, failType, summary)
	return nil
}

func (n *tuiNotifier) NotifyWaitingUser(run *swarm.SwarmRun, message string) error {
	log.Printf("[Swarm %s] Waiting for user: %s", run.ID, message)
	return nil
}

func (n *tuiNotifier) NotifyRunComplete(run *swarm.SwarmRun, report *swarm.SwarmReport) error {
	msg := fmt.Sprintf("[Swarm %s] Run completed: %s", run.ID, run.Status)
	if report != nil {
		msg += fmt.Sprintf(" (tasks: %d/%d)", report.Statistics.CompletedTasks, report.Statistics.TotalTasks)
	}
	log.Println(msg)
	return nil
}

func (n *tuiNotifier) NotifyDocumentForReview(run *swarm.SwarmRun, b64Data, fileName, mimeType, message string) error {
	log.Printf("[Swarm %s] Document for review: %s (%d bytes)", run.ID, fileName, len(b64Data))
	return nil
}
