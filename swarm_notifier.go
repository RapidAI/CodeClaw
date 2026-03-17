package main

import (
	"fmt"
	"log"
	"time"
)

// SwarmNotifier abstracts notification delivery for swarm events.
// Implementations push messages through the App's event system (WebSocket to
// frontend) and log them for observability.
type SwarmNotifier interface {
	// NotifyPhaseChange is called when a SwarmRun transitions to a new phase.
	NotifyPhaseChange(run *SwarmRun, phase SwarmPhase) error
	// NotifyAgentComplete is called when a SwarmAgent finishes its task.
	NotifyAgentComplete(run *SwarmRun, agent *SwarmAgent) error
	// NotifyFailure is called on compile or test failures.
	NotifyFailure(run *SwarmRun, failType string, summary string) error
	// NotifyWaitingUser is called when the run pauses for user confirmation.
	NotifyWaitingUser(run *SwarmRun, message string) error
	// NotifyRunComplete is called when the entire SwarmRun finishes.
	NotifyRunComplete(run *SwarmRun, report *SwarmReport) error
}

// EventEmitter is a function that pushes named events to the frontend.
// In production this wraps App.emitEvent; in tests a stub can be injected.
type EventEmitter func(name string, data ...interface{})

// DefaultSwarmNotifier delivers notifications via the App's event system
// (WebSocket push to the frontend) and writes structured log lines.
type DefaultSwarmNotifier struct {
	emit EventEmitter
}

// NewDefaultSwarmNotifier creates a notifier backed by the App infrastructure.
// It accepts an *App and uses its emitEvent method. If app is nil the emitter
// becomes a no-op (useful during early init).
func NewDefaultSwarmNotifier(app *App) *DefaultSwarmNotifier {
	var emitter EventEmitter
	if app != nil {
		emitter = func(name string, data ...interface{}) {
			app.emitEvent(name, data...)
		}
	} else {
		emitter = func(string, ...interface{}) {}
	}
	return &DefaultSwarmNotifier{emit: emitter}
}

// NewDefaultSwarmNotifierWithEmitter creates a notifier with a custom emitter.
// This is the preferred constructor for testing.
func NewDefaultSwarmNotifierWithEmitter(emit EventEmitter) *DefaultSwarmNotifier {
	if emit == nil {
		emit = func(string, ...interface{}) {}
	}
	return &DefaultSwarmNotifier{emit: emit}
}

// ---------------------------------------------------------------------------
// Interface implementation
// ---------------------------------------------------------------------------

// NotifyPhaseChange pushes a phase transition event.
// Validates: Requirements 8.1, 8.6
func (n *DefaultSwarmNotifier) NotifyPhaseChange(run *SwarmRun, phase SwarmPhase) error {
	completed := completedTaskCount(run)
	total := len(run.Tasks)
	msg := formatPhaseChangeMessage(run.ID, phase, completed, total)

	n.emit("swarm:phase_change", map[string]interface{}{
		"run_id":          run.ID,
		"phase":           string(phase),
		"completed_tasks": completed,
		"total_tasks":     total,
		"msg":             msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

// NotifyAgentComplete pushes an agent completion event.
// Validates: Requirements 8.2
func (n *DefaultSwarmNotifier) NotifyAgentComplete(run *SwarmRun, agent *SwarmAgent) error {
	duration := agentDuration(agent)
	msg := formatAgentCompleteMessage(run.ID, agent, duration)

	n.emit("swarm:agent_complete", map[string]interface{}{
		"run_id":           run.ID,
		"agent_id":         agent.ID,
		"role":             string(agent.Role),
		"task_index":       agent.TaskIndex,
		"status":           agent.Status,
		"duration_seconds": duration.Seconds(),
		"msg":              msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

// NotifyFailure pushes a failure event (compile or test).
// Validates: Requirements 8.3
func (n *DefaultSwarmNotifier) NotifyFailure(run *SwarmRun, failType string, summary string) error {
	msg := formatFailureMessage(run.ID, failType, summary)

	n.emit("swarm:failure", map[string]interface{}{
		"run_id":    run.ID,
		"fail_type": failType,
		"summary":   summary,
		"phase":     string(run.Phase),
		"msg":       msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

// NotifyWaitingUser pushes a user-input-required event.
// Validates: Requirements 8.4
func (n *DefaultSwarmNotifier) NotifyWaitingUser(run *SwarmRun, message string) error {
	msg := formatWaitingUserMessage(run.ID, message)

	n.emit("swarm:waiting_user", map[string]interface{}{
		"run_id":  run.ID,
		"message": message,
		"phase":   string(run.Phase),
		"msg":     msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

// NotifyRunComplete pushes a run-finished event with report statistics.
// Validates: Requirements 8.6
func (n *DefaultSwarmNotifier) NotifyRunComplete(run *SwarmRun, report *SwarmReport) error {
	msg := formatRunCompleteMessage(run, report)

	payload := map[string]interface{}{
		"run_id": run.ID,
		"status": string(run.Status),
		"mode":   string(run.Mode),
		"msg":    msg,
	}
	if report != nil {
		payload["total_tasks"] = report.Statistics.TotalTasks
		payload["completed_tasks"] = report.Statistics.CompletedTasks
		payload["failed_tasks"] = report.Statistics.FailedTasks
		payload["total_rounds"] = report.Statistics.TotalRounds
	}
	n.emit("swarm:run_complete", payload)
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

// ---------------------------------------------------------------------------
// Message formatting helpers
// ---------------------------------------------------------------------------

func formatPhaseChangeMessage(runID string, phase SwarmPhase, completed, total int) string {
	return fmt.Sprintf("[Swarm %s] Phase → %s (%d/%d tasks)", runID, phase, completed, total)
}

func formatAgentCompleteMessage(runID string, agent *SwarmAgent, dur time.Duration) string {
	return fmt.Sprintf("[Swarm %s] Agent %s (%s) completed task %d in %s",
		runID, agent.ID, agent.Role, agent.TaskIndex, dur.Truncate(time.Second))
}

func formatFailureMessage(runID string, failType, summary string) string {
	return fmt.Sprintf("[Swarm %s] %s failure: %s", runID, failType, summary)
}

func formatWaitingUserMessage(runID string, message string) string {
	return fmt.Sprintf("[Swarm %s] Waiting for user input: %s", runID, message)
}

func formatRunCompleteMessage(run *SwarmRun, report *SwarmReport) string {
	base := fmt.Sprintf("[Swarm %s] Run completed with status: %s", run.ID, run.Status)
	if report != nil {
		base += fmt.Sprintf(" (tasks: %d/%d, rounds: %d)",
			report.Statistics.CompletedTasks, report.Statistics.TotalTasks, report.Statistics.TotalRounds)
	}
	return base
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// completedTaskCount returns the number of agents in "completed" status.
func completedTaskCount(run *SwarmRun) int {
	count := 0
	for _, a := range run.Agents {
		if a.Status == "completed" {
			count++
		}
	}
	return count
}

// agentDuration computes the elapsed time for an agent. Returns zero if
// timing information is unavailable.
func agentDuration(agent *SwarmAgent) time.Duration {
	if agent.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if agent.CompletedAt != nil {
		end = *agent.CompletedAt
	}
	return end.Sub(*agent.StartedAt)
}

// ---------------------------------------------------------------------------
// NoopSwarmNotifier – no-op implementation for testing
// ---------------------------------------------------------------------------

// NoopSwarmNotifier silently discards all notifications. Useful in unit tests
// where notification side-effects are irrelevant.
type NoopSwarmNotifier struct{}

func (n *NoopSwarmNotifier) NotifyPhaseChange(*SwarmRun, SwarmPhase) error   { return nil }
func (n *NoopSwarmNotifier) NotifyAgentComplete(*SwarmRun, *SwarmAgent) error { return nil }
func (n *NoopSwarmNotifier) NotifyFailure(*SwarmRun, string, string) error    { return nil }
func (n *NoopSwarmNotifier) NotifyWaitingUser(*SwarmRun, string) error        { return nil }
func (n *NoopSwarmNotifier) NotifyRunComplete(*SwarmRun, *SwarmReport) error  { return nil }
