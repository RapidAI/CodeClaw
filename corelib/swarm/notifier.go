package swarm

import (
	"fmt"
	"log"
	"time"
)

// Notifier abstracts notification delivery for swarm events.
type Notifier interface {
	NotifyPhaseChange(run *SwarmRun, phase SwarmPhase) error
	NotifyAgentComplete(run *SwarmRun, agent *SwarmAgent) error
	NotifyFailure(run *SwarmRun, failType string, summary string) error
	NotifyWaitingUser(run *SwarmRun, message string) error
	NotifyRunComplete(run *SwarmRun, report *SwarmReport) error
	NotifyDocumentForReview(run *SwarmRun, b64Data, fileName, mimeType, message string) error
}

// SwarmEventEmitter is a function that pushes named events to the frontend.
type SwarmEventEmitter func(name string, data ...interface{})

// IMFileDeliveryFunc delivers a file via IM channel.
type IMFileDeliveryFunc func(b64Data, fileName, mimeType, message string)

// DefaultNotifier delivers notifications via an event emitter and log.
type DefaultNotifier struct {
	emit           SwarmEventEmitter
	imFileDelivery IMFileDeliveryFunc
	imTextDelivery func(text string)
}

// NewDefaultNotifier creates a notifier with a custom emitter.
func NewDefaultNotifier(emit SwarmEventEmitter) *DefaultNotifier {
	if emit == nil {
		emit = func(string, ...interface{}) {}
	}
	return &DefaultNotifier{emit: emit}
}

// SetIMDelivery sets IM delivery callbacks.
func (n *DefaultNotifier) SetIMDelivery(fileFn IMFileDeliveryFunc, textFn func(string)) {
	n.imFileDelivery = fileFn
	n.imTextDelivery = textFn
}

func (n *DefaultNotifier) NotifyPhaseChange(run *SwarmRun, phase SwarmPhase) error {
	completed := CompletedTaskCount(run)
	total := len(run.Tasks)
	msg := fmt.Sprintf("[Swarm %s] Phase → %s (%d/%d tasks)", run.ID, phase, completed, total)
	n.emit("swarm:phase_change", map[string]interface{}{
		"run_id": run.ID, "phase": string(phase),
		"completed_tasks": completed, "total_tasks": total, "msg": msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

func (n *DefaultNotifier) NotifyAgentComplete(run *SwarmRun, agent *SwarmAgent) error {
	duration := AgentDuration(agent)
	msg := fmt.Sprintf("[Swarm %s] Agent %s (%s) completed task %d in %s",
		run.ID, agent.ID, agent.Role, agent.TaskIndex, duration.Truncate(time.Second))
	n.emit("swarm:agent_complete", map[string]interface{}{
		"run_id": run.ID, "agent_id": agent.ID, "role": string(agent.Role),
		"task_index": agent.TaskIndex, "status": agent.Status,
		"duration_seconds": duration.Seconds(), "msg": msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

func (n *DefaultNotifier) NotifyFailure(run *SwarmRun, failType string, summary string) error {
	msg := fmt.Sprintf("[Swarm %s] %s failure: %s", run.ID, failType, summary)
	n.emit("swarm:failure", map[string]interface{}{
		"run_id": run.ID, "fail_type": failType, "summary": summary,
		"phase": string(run.Phase), "msg": msg,
	})
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

func (n *DefaultNotifier) NotifyWaitingUser(run *SwarmRun, message string) error {
	msg := fmt.Sprintf("[Swarm %s] Waiting for user input: %s", run.ID, message)
	n.emit("swarm:waiting_user", map[string]interface{}{
		"run_id": run.ID, "message": message, "phase": string(run.Phase), "msg": msg,
	})
	if n.imTextDelivery != nil {
		n.imTextDelivery(message)
	}
	log.Printf("[SwarmNotifier] %s", msg)
	return nil
}

func (n *DefaultNotifier) NotifyRunComplete(run *SwarmRun, report *SwarmReport) error {
	base := fmt.Sprintf("[Swarm %s] Run completed with status: %s", run.ID, run.Status)
	if report != nil {
		base += fmt.Sprintf(" (tasks: %d/%d, rounds: %d)",
			report.Statistics.CompletedTasks, report.Statistics.TotalTasks, report.Statistics.TotalRounds)
	}
	payload := map[string]interface{}{
		"run_id": run.ID, "status": string(run.Status), "mode": string(run.Mode), "msg": base,
	}
	if report != nil {
		payload["total_tasks"] = report.Statistics.TotalTasks
		payload["completed_tasks"] = report.Statistics.CompletedTasks
		payload["failed_tasks"] = report.Statistics.FailedTasks
		payload["total_rounds"] = report.Statistics.TotalRounds
	}
	n.emit("swarm:run_complete", payload)
	log.Printf("[SwarmNotifier] %s", base)
	return nil
}

func (n *DefaultNotifier) NotifyDocumentForReview(run *SwarmRun, b64Data, fileName, mimeType, message string) error {
	n.emit("swarm:document_review", map[string]interface{}{
		"run_id": run.ID, "file_data": b64Data, "file_name": fileName,
		"mime_type": mimeType, "message": message, "phase": string(run.Phase),
	})
	if n.imFileDelivery != nil {
		n.imFileDelivery(b64Data, fileName, mimeType, message)
	}
	log.Printf("[SwarmNotifier] [Swarm %s] 发送审阅文档: %s (%d bytes)", run.ID, fileName, len(b64Data))
	return nil
}

// CompletedTaskCount returns the number of agents in "completed" status.
func CompletedTaskCount(run *SwarmRun) int {
	count := 0
	for _, a := range run.Agents {
		if a.Status == "completed" {
			count++
		}
	}
	return count
}

// AgentDuration computes the elapsed time for an agent.
func AgentDuration(agent *SwarmAgent) time.Duration {
	if agent.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if agent.CompletedAt != nil {
		end = *agent.CompletedAt
	}
	return end.Sub(*agent.StartedAt)
}

// NoopNotifier silently discards all notifications.
type NoopNotifier struct{}

func (n *NoopNotifier) NotifyPhaseChange(*SwarmRun, SwarmPhase) error                          { return nil }
func (n *NoopNotifier) NotifyAgentComplete(*SwarmRun, *SwarmAgent) error                       { return nil }
func (n *NoopNotifier) NotifyFailure(*SwarmRun, string, string) error                          { return nil }
func (n *NoopNotifier) NotifyWaitingUser(*SwarmRun, string) error                              { return nil }
func (n *NoopNotifier) NotifyRunComplete(*SwarmRun, *SwarmReport) error                        { return nil }
func (n *NoopNotifier) NotifyDocumentForReview(*SwarmRun, string, string, string, string) error { return nil }
