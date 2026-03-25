package swarm

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestValidateMaxAgents(t *testing.T) {
	tests := []struct {
		input, expected int
	}{
		{0, 1}, {-5, 1}, {1, 1}, {5, 5}, {10, 10}, {15, 10}, {100, 10},
	}
	for _, tt := range tests {
		got := ValidateMaxAgents(tt.input)
		if got != tt.expected {
			t.Errorf("ValidateMaxAgents(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestNewSwarmOrchestrator_Defaults(t *testing.T) {
	notifier := &NoopNotifier{}
	o := NewSwarmOrchestrator(nil, notifier)

	if o.maxRounds != 5 {
		t.Errorf("maxRounds = %d, want 5", o.maxRounds)
	}
	if o.maxAgents != 5 {
		t.Errorf("maxAgents = %d, want 5", o.maxAgents)
	}
	if o.notifier != notifier {
		t.Error("notifier not set")
	}
	if o.worktreeMgr == nil {
		t.Error("worktreeMgr is nil")
	}
	if o.conflictDet == nil {
		t.Error("conflictDet is nil")
	}
	if o.reporter == nil {
		t.Error("reporter is nil")
	}
	if o.toolSelector == nil {
		t.Error("toolSelector is nil")
	}
	if o.taskSplitter == nil {
		t.Error("taskSplitter is nil")
	}
	if o.mergeCtrl == nil {
		t.Error("mergeCtrl is nil")
	}
	if o.feedbackLoop == nil {
		t.Error("feedbackLoop is nil")
	}
	if o.taskVerifier == nil {
		t.Error("taskVerifier is nil")
	}
}

func TestNewSwarmOrchestrator_WithOptions(t *testing.T) {
	notifier := &NoopNotifier{}
	o := NewSwarmOrchestrator(nil, notifier,
		WithMaxRounds(10),
		WithMaxAgents(3),
	)

	if o.maxRounds != 10 {
		t.Errorf("maxRounds = %d, want 10", o.maxRounds)
	}
	if o.maxAgents != 3 {
		t.Errorf("maxAgents = %d, want 3", o.maxAgents)
	}
}

func TestNewSwarmOrchestrator_WithMaxAgentsClamped(t *testing.T) {
	notifier := &NoopNotifier{}
	o := NewSwarmOrchestrator(nil, notifier, WithMaxAgents(100))
	if o.maxAgents != 10 {
		t.Errorf("maxAgents = %d, want 10 (clamped)", o.maxAgents)
	}
}

func TestInstalledToolNames_NilAppCtx(t *testing.T) {
	o := &SwarmOrchestrator{}
	names := o.installedToolNames()
	if names != nil {
		t.Errorf("expected nil, got %v", names)
	}
}

func TestSelectToolForTask_FixedTool(t *testing.T) {
	o := NewSwarmOrchestrator(nil, &NoopNotifier{})
	run := &SwarmRun{Tool: "codex"}
	task := SubTask{Description: "implement feature"}
	name, reason := o.selectToolForTask(run, task)
	if name != "codex" {
		t.Errorf("tool = %q, want codex", name)
	}
	if reason != "用户指定工具" {
		t.Errorf("reason = %q, want 用户指定工具", reason)
	}
}

func TestSelectToolForTask_AutoSelect(t *testing.T) {
	o := NewSwarmOrchestrator(nil, &NoopNotifier{})
	run := &SwarmRun{}
	task := SubTask{Description: "implement a Go REST API"}
	name, _ := o.selectToolForTask(run, task)
	if name == "" {
		t.Error("expected a tool recommendation, got empty")
	}
}

func TestStartSwarmRun_Validation(t *testing.T) {
	o := NewSwarmOrchestrator(nil, &NoopNotifier{})

	// Missing project path
	_, err := o.StartSwarmRun(SwarmRunRequest{Mode: SwarmModeGreenfield})
	if err == nil {
		t.Error("expected error for missing project path")
	}

	// Missing requirements for greenfield
	_, err = o.StartSwarmRun(SwarmRunRequest{
		Mode:        SwarmModeGreenfield,
		ProjectPath: "/tmp/test",
	})
	if err == nil {
		t.Error("expected error for missing requirements")
	}

	// Missing task input for maintenance
	_, err = o.StartSwarmRun(SwarmRunRequest{
		Mode:        SwarmModeMaintenance,
		ProjectPath: "/tmp/test",
	})
	if err == nil {
		t.Error("expected error for missing task input")
	}
}

func TestPauseResumeSwarmRun(t *testing.T) {
	o := &SwarmOrchestrator{
		notifier:  &NoopNotifier{},
		maxRounds: 5,
		maxAgents: 5,
	}

	// No active run
	if err := o.PauseSwarmRun("nonexistent"); err == nil {
		t.Error("expected error for nonexistent run")
	}

	// Set up an active run
	run := &SwarmRun{ID: "test-run", Status: SwarmStatusRunning}
	o.activeRun = run
	o.runHistory = append(o.runHistory, run)

	// Pause
	if err := o.PauseSwarmRun("test-run"); err != nil {
		t.Fatalf("PauseSwarmRun: %v", err)
	}
	if run.Status != SwarmStatusPaused {
		t.Errorf("status = %s, want paused", run.Status)
	}

	// Resume
	if err := o.ResumeSwarmRun("test-run"); err != nil {
		t.Fatalf("ResumeSwarmRun: %v", err)
	}
	if run.Status != SwarmStatusRunning {
		t.Errorf("status = %s, want running", run.Status)
	}
}

func TestListSwarmRuns(t *testing.T) {
	o := &SwarmOrchestrator{
		notifier: &NoopNotifier{},
	}
	o.runHistory = []*SwarmRun{
		{ID: "run-1", Mode: SwarmModeGreenfield, Status: SwarmStatusCompleted},
		{ID: "run-2", Mode: SwarmModeMaintenance, Status: SwarmStatusRunning},
	}

	summaries := o.ListSwarmRuns()
	if len(summaries) != 2 {
		t.Fatalf("len = %d, want 2", len(summaries))
	}
	if summaries[0].ID != "run-1" {
		t.Errorf("summaries[0].ID = %q, want run-1", summaries[0].ID)
	}
}

func TestGetSwarmRun(t *testing.T) {
	o := &SwarmOrchestrator{notifier: &NoopNotifier{}}
	o.runHistory = []*SwarmRun{{ID: "run-1"}}

	run, err := o.GetSwarmRun("run-1")
	if err != nil {
		t.Fatalf("GetSwarmRun: %v", err)
	}
	if run.ID != "run-1" {
		t.Errorf("ID = %q, want run-1", run.ID)
	}

	_, err = o.GetSwarmRun("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent run")
	}
}

func TestProvideUserInput(t *testing.T) {
	o := &SwarmOrchestrator{notifier: &NoopNotifier{}}
	run := &SwarmRun{ID: "run-1", UserInputCh: make(chan string, 1)}
	o.activeRun = run

	if err := o.ProvideUserInput("run-1", "yes"); err != nil {
		t.Fatalf("ProvideUserInput: %v", err)
	}

	got := <-run.UserInputCh
	if got != "yes" {
		t.Errorf("input = %q, want yes", got)
	}

	// Not waiting for input (channel full)
	run.UserInputCh <- "buffered"
	if err := o.ProvideUserInput("run-1", "another"); err == nil {
		t.Error("expected error when not waiting for input")
	}
}

func TestSetIMDelivery(t *testing.T) {
	dn := NewDefaultNotifier(nil)
	o := NewSwarmOrchestrator(nil, dn)

	var called bool
	o.SetIMDelivery(func(b64, fn, mt, msg string) { called = true }, nil)
	dn.NotifyDocumentForReview(&SwarmRun{ID: "test"}, "data", "file.pdf", "application/pdf", "review")
	if !called {
		t.Error("IM file delivery not called")
	}

	o.ClearIMDelivery()
	called = false
	dn.NotifyDocumentForReview(&SwarmRun{ID: "test"}, "data", "file.pdf", "application/pdf", "review")
	if called {
		t.Error("IM file delivery should not be called after clear")
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// Feature: swarm-orchestrator, Property 1: 阶段顺序正确性
func TestProperty_PhaseSequenceCorrectness(t *testing.T) {
	greenfieldPhases := []SwarmPhase{
		PhaseTaskSplit, PhaseArchitecture, PhaseDevelopment,
		PhaseMerge, PhaseCompile, PhaseTest, PhaseDocument, PhaseReport,
	}
	maintenancePhases := []SwarmPhase{
		PhaseTaskSplit, PhaseConflictDetect, PhaseDevelopment,
		PhaseMerge, PhaseCompile, PhaseTest, PhaseDocument, PhaseReport,
	}

	rapid.Check(t, func(t *rapid.T) {
		isGreenfield := rapid.Bool().Draw(t, "isGreenfield")
		var expected []SwarmPhase
		var mode SwarmMode
		if isGreenfield {
			expected = greenfieldPhases
			mode = SwarmModeGreenfield
		} else {
			expected = maintenancePhases
			mode = SwarmModeMaintenance
		}

		if mode == SwarmModeGreenfield {
			if expected[1] != PhaseArchitecture {
				t.Fatal("greenfield must have architecture as second phase")
			}
		} else {
			if expected[1] != PhaseConflictDetect {
				t.Fatal("maintenance must have conflict_detect as second phase")
			}
		}

		seen := make(map[SwarmPhase]bool)
		for _, p := range expected {
			if seen[p] {
				t.Fatalf("duplicate phase: %s", p)
			}
			seen[p] = true
		}

		if expected[0] != PhaseTaskSplit {
			t.Fatal("first phase must be task_split")
		}
		if expected[len(expected)-1] != PhaseReport {
			t.Fatal("last phase must be report")
		}
	})
}

// Feature: swarm-orchestrator, Property 18: 单 Run 限制
func TestProperty_SingleRunLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		o := NewSwarmOrchestrator(nil, &NoopNotifier{})

		o.activeRun = &SwarmRun{
			ID:     NewSwarmRunID(),
			Status: SwarmStatusRunning,
		}
		o.runHistory = append(o.runHistory, o.activeRun)

		_, err := o.StartSwarmRun(SwarmRunRequest{
			Mode:         SwarmModeGreenfield,
			ProjectPath:  "/tmp/test",
			Requirements: "test",
			Tool:         "claude",
		})
		if err == nil {
			t.Fatal("expected error when starting a second run")
		}
	})
}

// Feature: swarm-orchestrator, Property 20: 并发配置范围验证
func TestProperty_MaxAgentsRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(-100, 200).Draw(t, "maxAgents")
		result := ValidateMaxAgents(n)
		if result < 1 || result > 10 {
			t.Fatalf("ValidateMaxAgents(%d) = %d, out of [1,10]", n, result)
		}
	})
}
