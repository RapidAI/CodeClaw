package swarm

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Unit tests for createAgent
// ---------------------------------------------------------------------------

func TestCreateAgent_NoManager(t *testing.T) {
	o := &SwarmOrchestrator{maxAgents: 5, maxRounds: 5}
	run := &SwarmRun{ID: "test-run-1", ProjectPath: "/tmp/proj"}

	agent, err := o.createAgent(run, RoleDeveloper, 0, "/tmp/wt", "swarm/test/dev-0", "claude", PromptContext{
		ProjectName: "test",
		TechStack:   "Go",
		TaskDesc:    "implement feature X",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != "completed" {
		t.Errorf("expected completed status in test mode, got %s", agent.Status)
	}
	if agent.Role != RoleDeveloper {
		t.Errorf("expected developer role, got %s", agent.Role)
	}
	if agent.WorktreePath != "/tmp/wt" {
		t.Errorf("expected worktree path /tmp/wt, got %s", agent.WorktreePath)
	}
	if agent.BranchName != "swarm/test/dev-0" {
		t.Errorf("expected branch swarm/test/dev-0, got %s", agent.BranchName)
	}
}

func TestCreateAgent_InvalidRole(t *testing.T) {
	o := &SwarmOrchestrator{maxAgents: 5, maxRounds: 5}
	run := &SwarmRun{ID: "test-run-2", ProjectPath: "/tmp/proj"}

	_, err := o.createAgent(run, AgentRole("unknown"), 0, "/tmp/wt", "swarm/test/unk-0", "claude", PromptContext{})
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestCreateAgent_AgentIDFormat(t *testing.T) {
	o := &SwarmOrchestrator{maxAgents: 5, maxRounds: 5}
	run := &SwarmRun{ID: "run-abc", ProjectPath: "/tmp/proj"}

	agent, err := o.createAgent(run, RoleArchitect, 3, "/tmp/wt", "swarm/run-abc/arch-3", "claude", PromptContext{
		ProjectName:  "proj",
		TechStack:    "Go",
		Requirements: "build something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "run-abc-architect-3"
	if agent.ID != expected {
		t.Errorf("expected agent ID %q, got %q", expected, agent.ID)
	}
}

// ---------------------------------------------------------------------------
// Unit tests for waitForAgent
// ---------------------------------------------------------------------------

func TestWaitForAgent_NoManager(t *testing.T) {
	o := &SwarmOrchestrator{}
	run := &SwarmRun{ID: "test-run"}
	agent := &SwarmAgent{ID: "agent-1", SessionID: ""}

	err := o.waitForAgent(run, agent, time.Second)
	if err != nil {
		t.Fatalf("expected nil error for no-manager mode, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests for runDeveloperAgents
// ---------------------------------------------------------------------------

func TestRunDeveloperAgents_EmptyTasks(t *testing.T) {
	notifier := &NoopNotifier{}
	o := &SwarmOrchestrator{
		maxAgents:   5,
		maxRounds:   5,
		worktreeMgr: NewWorktreeManager(),
		notifier:    notifier,
	}
	run := &SwarmRun{
		ID:          "test-run",
		ProjectPath: "/tmp/proj",
		Status:      SwarmStatusRunning,
	}

	err := o.runDeveloperAgents(run, nil, 5, "claude", "")
	if err != nil {
		t.Fatalf("unexpected error for empty tasks: %v", err)
	}
}

func TestRunDeveloperAgents_CancelledRun(t *testing.T) {
	notifier := &NoopNotifier{}
	o := &SwarmOrchestrator{
		maxAgents:   5,
		maxRounds:   5,
		worktreeMgr: NewWorktreeManager(),
		notifier:    notifier,
	}
	run := &SwarmRun{
		ID:          "test-run",
		ProjectPath: "/tmp/proj",
		Status:      SwarmStatusCancelled,
	}

	tasks := []SubTask{
		{Index: 0, Description: "task 0"},
		{Index: 1, Description: "task 1"},
	}

	err := o.runDeveloperAgents(run, tasks, 5, "claude", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No agents should have been created since run was cancelled.
	if len(run.Agents) != 0 {
		t.Errorf("expected 0 agents for cancelled run, got %d", len(run.Agents))
	}
}

func TestRunDeveloperAgents_MaxAgentsClamped(t *testing.T) {
	// Verify that invalid maxAgents values are clamped.
	tests := []struct {
		input    int
		expected int
	}{
		{0, 1}, {-1, 1}, {15, 10}, {5, 5},
	}
	for _, tt := range tests {
		got := ValidateMaxAgents(tt.input)
		if got != tt.expected {
			t.Errorf("ValidateMaxAgents(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Test concurrency control: verify semaphore limits concurrent goroutines
// ---------------------------------------------------------------------------

func TestConcurrencyControl_SemaphoreLimit(t *testing.T) {
	maxConcurrent := 3
	sem := make(chan struct{}, maxConcurrent)
	var peak int64
	var current int64
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			c := atomic.AddInt64(&current, 1)
			// Track peak concurrency.
			for {
				p := atomic.LoadInt64(&peak)
				if c <= p || atomic.CompareAndSwapInt64(&peak, p, c) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt64(&current, -1)
		}()
	}

	wg.Wait()

	if peak > int64(maxConcurrent) {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, maxConcurrent)
	}
}

// ---------------------------------------------------------------------------
// Test retry logic constants
// ---------------------------------------------------------------------------

func TestRetryConstants(t *testing.T) {
	if MaxAgentRetries != 2 {
		t.Errorf("MaxAgentRetries = %d, want 2", MaxAgentRetries)
	}
	if DefaultAgentTimeout != 30*time.Minute {
		t.Errorf("DefaultAgentTimeout = %v, want 30m", DefaultAgentTimeout)
	}
	if DefaultMaxDeveloperAgents != 5 {
		t.Errorf("DefaultMaxDeveloperAgents = %d, want 5", DefaultMaxDeveloperAgents)
	}
}
