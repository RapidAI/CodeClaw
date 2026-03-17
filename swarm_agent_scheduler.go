package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Default agent configuration constants.
const (
	DefaultMaxDeveloperAgents = 5
	MinMaxDeveloperAgents     = 1
	MaxMaxDeveloperAgents     = 10
	DefaultAgentTimeout       = 30 * time.Minute
	MaxAgentRetries           = 2
	agentPollInterval         = 3 * time.Second
)

// createAgent creates a single SwarmAgent backed by a RemoteSession.
// It renders the role-specific system prompt, creates a RemoteSession via
// RemoteSessionManager.Create with ProjectPath pointing to the worktree,
// and returns the populated SwarmAgent.
func (o *SwarmOrchestrator) createAgent(
	run *SwarmRun,
	role AgentRole,
	taskIndex int,
	worktreePath string,
	branchName string,
	tool string,
	ctx PromptContext,
) (*SwarmAgent, error) {
	prompt, err := RenderPrompt(role, ctx)
	if err != nil {
		return nil, fmt.Errorf("render prompt for %s: %w", role, err)
	}

	agent := &SwarmAgent{
		ID:           fmt.Sprintf("%s-%s-%d", run.ID, role, taskIndex),
		Role:         role,
		TaskIndex:    taskIndex,
		WorktreePath: worktreePath,
		BranchName:   branchName,
		Status:       "pending",
	}

	if o.manager == nil {
		// No session manager (testing mode) — mark as completed immediately.
		log.Printf("[SwarmScheduler] no session manager, skipping agent %s", agent.ID)
		now := time.Now()
		agent.Status = "completed"
		agent.StartedAt = &now
		agent.CompletedAt = &now
		agent.Output = prompt
		return agent, nil
	}

	spec := LaunchSpec{
		Tool:        tool,
		ProjectPath: worktreePath,
		Env: map[string]string{
			"SWARM_SYSTEM_PROMPT": prompt,
			"SWARM_ROLE":         string(role),
			"SWARM_RUN_ID":       run.ID,
			"SWARM_TASK_INDEX":   fmt.Sprintf("%d", taskIndex),
		},
	}

	session, err := o.manager.Create(spec)
	if err != nil {
		return nil, fmt.Errorf("create session for %s: %w", agent.ID, err)
	}

	now := time.Now()
	agent.SessionID = session.ID
	agent.Status = "running"
	agent.StartedAt = &now

	o.addTimelineEvent(run, "agent_created",
		fmt.Sprintf("Created %s agent (task %d) → session %s", role, taskIndex, session.ID),
		agent.ID)

	return agent, nil
}

// waitForAgent polls the RemoteSession status until the agent completes,
// errors out, or the timeout expires. On timeout the session is killed and
// the agent is marked as failed.
func (o *SwarmOrchestrator) waitForAgent(run *SwarmRun, agent *SwarmAgent, timeout time.Duration) error {
	if o.manager == nil || agent.SessionID == "" {
		return nil // nothing to wait for in test mode
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(agentPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// Timeout — kill the session and mark agent as failed.
			log.Printf("[SwarmScheduler] agent %s timed out after %v", agent.ID, timeout)
			_ = o.manager.Kill(agent.SessionID)
			now := time.Now()
			agent.Status = "failed"
			agent.Error = fmt.Sprintf("agent timed out after %v", timeout)
			agent.CompletedAt = &now
			o.addTimelineEvent(run, "agent_timeout",
				fmt.Sprintf("Agent %s timed out after %v", agent.ID, timeout), agent.ID)
			return fmt.Errorf("agent %s timed out", agent.ID)

		case <-ticker.C:
			session, ok := o.manager.Get(agent.SessionID)
			if !ok {
				now := time.Now()
				agent.Status = "failed"
				agent.Error = "session not found"
				agent.CompletedAt = &now
				return fmt.Errorf("session %s not found", agent.SessionID)
			}

			session.mu.RLock()
			status := session.Status
			session.mu.RUnlock()

			switch status {
			case SessionExited:
				// Agent completed successfully.
				now := time.Now()
				agent.Status = "completed"
				agent.CompletedAt = &now
				session.mu.RLock()
				agent.Output = session.Summary.ProgressSummary
				session.mu.RUnlock()
				return nil

			case SessionError:
				// Agent encountered an error.
				now := time.Now()
				agent.Status = "failed"
				agent.CompletedAt = &now
				session.mu.RLock()
				agent.Error = session.Summary.LastResult
				session.mu.RUnlock()
				return fmt.Errorf("agent %s session error: %s", agent.ID, agent.Error)

			default:
				// Still running — check if the run was cancelled.
				if run.Status == SwarmStatusCancelled {
					_ = o.manager.Kill(agent.SessionID)
					now := time.Now()
					agent.Status = "failed"
					agent.Error = "run cancelled"
					agent.CompletedAt = &now
					return fmt.Errorf("run cancelled")
				}
				// Continue polling.
			}
		}
	}
}

// runDeveloperAgents runs developer agents for the given tasks with
// concurrency control. A buffered channel acts as a semaphore to limit
// the number of simultaneously active Developer agents to maxAgents.
// Tasks that exceed the concurrency limit are queued and dispatched as
// slots become available. Each agent has a timeout (DefaultAgentTimeout)
// and will be retried up to MaxAgentRetries times on error.
func (o *SwarmOrchestrator) runDeveloperAgents(
	run *SwarmRun,
	tasks []SubTask,
	maxAgents int,
	tool string,
	archDesign string,
) error {
	maxAgents = ValidateMaxAgents(maxAgents)

	sem := make(chan struct{}, maxAgents)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, task := range tasks {
		// Check if run is cancelled before scheduling.
		if run.Status == SwarmStatusCancelled {
			break
		}
		// Wait while paused.
		for run.Status == SwarmStatusPaused {
			time.Sleep(time.Second)
		}

		sem <- struct{}{} // acquire semaphore slot (blocks if at capacity)
		wg.Add(1)

		go func(t SubTask) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore slot

			err := o.runDeveloperAgentWithRetry(run, t, tool, archDesign, &mu)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(task)
	}

	wg.Wait()

	// We don't fail the whole phase for individual agent failures — the
	// merge phase will handle partial results. Log if there were errors.
	if firstErr != nil {
		log.Printf("[SwarmScheduler] some developer agents failed: %v", firstErr)
	}
	return nil
}

// runDeveloperAgentWithRetry creates a developer agent, waits for it, and
// retries up to MaxAgentRetries times on error.
func (o *SwarmOrchestrator) runDeveloperAgentWithRetry(
	run *SwarmRun,
	task SubTask,
	tool string,
	archDesign string,
	mu *sync.Mutex,
) error {
	branchName := fmt.Sprintf("swarm/%s/developer-%d", run.ID, task.Index)

	wt, err := o.worktreeMgr.CreateWorktree(run.ProjectPath, run.ID, branchName)
	if err != nil {
		log.Printf("[SwarmScheduler] create worktree for task %d: %v", task.Index, err)
		return fmt.Errorf("create worktree: %w", err)
	}

	ctx := PromptContext{
		ProjectName: run.ProjectPath,
		TechStack:   run.TechStack,
		TaskDesc:    task.Description,
		ArchDesign:  archDesign,
	}

	var agent *SwarmAgent
	var lastErr error

	for attempt := 0; attempt <= MaxAgentRetries; attempt++ {
		if run.Status == SwarmStatusCancelled {
			return fmt.Errorf("run cancelled")
		}

		agent, err = o.createAgent(run, RoleDeveloper, task.Index, wt.Path, branchName, tool, ctx)
		if err != nil {
			lastErr = err
			log.Printf("[SwarmScheduler] create agent attempt %d for task %d failed: %v",
				attempt+1, task.Index, err)
			if attempt < MaxAgentRetries {
				agent = &SwarmAgent{
					ID:         fmt.Sprintf("%s-developer-%d", run.ID, task.Index),
					Role:       RoleDeveloper,
					TaskIndex:  task.Index,
					RetryCount: attempt + 1,
					Status:     "pending",
				}
				continue
			}
			break
		}

		// Register agent in the run.
		mu.Lock()
		agent.RetryCount = attempt
		run.Agents = append(run.Agents, *agent)
		agentIdx := len(run.Agents) - 1
		mu.Unlock()

		// Wait for agent completion with timeout.
		waitErr := o.waitForAgent(run, agent, DefaultAgentTimeout)

		mu.Lock()
		run.Agents[agentIdx] = *agent // sync back status changes
		mu.Unlock()

		if waitErr == nil {
			// Agent completed successfully.
			_ = o.notifier.NotifyAgentComplete(run, agent)
			return nil
		}

		lastErr = waitErr
		log.Printf("[SwarmScheduler] agent %s failed (attempt %d/%d): %v",
			agent.ID, attempt+1, MaxAgentRetries+1, waitErr)

		// Only retry if the agent errored (not timed out) and we have retries left.
		if agent.Status == "failed" && attempt < MaxAgentRetries {
			o.addTimelineEvent(run, "agent_retry",
				fmt.Sprintf("Retrying agent %s (attempt %d)", agent.ID, attempt+2), agent.ID)
			agent.RetryCount = attempt + 1
			mu.Lock()
			run.Agents[agentIdx].RetryCount = attempt + 1
			mu.Unlock()
			continue
		}
		break
	}

	// All retries exhausted.
	if agent != nil {
		_ = o.notifier.NotifyFailure(run, "agent_failed",
			fmt.Sprintf("Agent for task %d failed after %d attempts: %v",
				task.Index, agent.RetryCount+1, lastErr))
	}
	return lastErr
}
