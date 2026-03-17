package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// capturedEvent records a single emitted event for assertion.
type capturedEvent struct {
	Name string
	Data []interface{}
}

// eventCapture collects events emitted by a DefaultSwarmNotifier.
type eventCapture struct {
	mu     sync.Mutex
	events []capturedEvent
}

func (c *eventCapture) emitter() EventEmitter {
	return func(name string, data ...interface{}) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.events = append(c.events, capturedEvent{Name: name, Data: data})
	}
}

func (c *eventCapture) last() capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return capturedEvent{}
	}
	return c.events[len(c.events)-1]
}

func (c *eventCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func makeTestRun() *SwarmRun {
	return &SwarmRun{
		ID:     "swarm_test_001",
		Mode:   SwarmModeGreenfield,
		Status: SwarmStatusRunning,
		Phase:  PhaseDevelopment,
		Tasks: []SubTask{
			{Index: 0, Description: "task-a"},
			{Index: 1, Description: "task-b"},
			{Index: 2, Description: "task-c"},
		},
		Agents: []SwarmAgent{
			{ID: "a1", Role: RoleDeveloper, Status: "completed"},
			{ID: "a2", Role: RoleDeveloper, Status: "running"},
		},
	}
}

// ---------------------------------------------------------------------------
// Unit tests – DefaultSwarmNotifier
// ---------------------------------------------------------------------------

func TestNotifyPhaseChange(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()

	err := n.NotifyPhaseChange(run, PhaseCompile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.count() != 1 {
		t.Fatalf("expected 1 event, got %d", cap.count())
	}
	ev := cap.last()
	if ev.Name != "swarm:phase_change" {
		t.Errorf("event name = %q, want swarm:phase_change", ev.Name)
	}
	payload := ev.Data[0].(map[string]interface{})
	if payload["run_id"] != run.ID {
		t.Errorf("run_id = %v, want %s", payload["run_id"], run.ID)
	}
	if payload["phase"] != string(PhaseCompile) {
		t.Errorf("phase = %v, want %s", payload["phase"], PhaseCompile)
	}
	// 1 agent is completed out of 3 tasks
	if payload["completed_tasks"] != 1 {
		t.Errorf("completed_tasks = %v, want 1", payload["completed_tasks"])
	}
	if payload["total_tasks"] != 3 {
		t.Errorf("total_tasks = %v, want 3", payload["total_tasks"])
	}
	msg := payload["msg"].(string)
	if !strings.Contains(msg, "Phase") || !strings.Contains(msg, string(PhaseCompile)) {
		t.Errorf("msg missing phase info: %s", msg)
	}
}

func TestNotifyAgentComplete(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()

	start := time.Now().Add(-5 * time.Minute)
	end := time.Now()
	agent := &SwarmAgent{
		ID:          "dev-1",
		Role:        RoleDeveloper,
		TaskIndex:   2,
		Status:      "completed",
		StartedAt:   &start,
		CompletedAt: &end,
	}

	err := n.NotifyAgentComplete(run, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ev := cap.last()
	if ev.Name != "swarm:agent_complete" {
		t.Errorf("event name = %q, want swarm:agent_complete", ev.Name)
	}
	payload := ev.Data[0].(map[string]interface{})
	if payload["agent_id"] != "dev-1" {
		t.Errorf("agent_id = %v, want dev-1", payload["agent_id"])
	}
	if payload["role"] != string(RoleDeveloper) {
		t.Errorf("role = %v, want developer", payload["role"])
	}
	dur := payload["duration_seconds"].(float64)
	if dur < 290 || dur > 310 {
		t.Errorf("duration_seconds = %v, expected ~300", dur)
	}
	msg := payload["msg"].(string)
	if !strings.Contains(msg, "dev-1") || !strings.Contains(msg, "developer") {
		t.Errorf("msg missing agent info: %s", msg)
	}
}

func TestNotifyAgentComplete_NoDuration(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()

	agent := &SwarmAgent{
		ID:        "dev-2",
		Role:      RoleDeveloper,
		TaskIndex: 0,
		Status:    "completed",
	}

	err := n.NotifyAgentComplete(run, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := cap.last().Data[0].(map[string]interface{})
	dur := payload["duration_seconds"].(float64)
	if dur != 0 {
		t.Errorf("duration_seconds = %v, want 0 when no StartedAt", dur)
	}
}

func TestNotifyFailure(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()

	err := n.NotifyFailure(run, "compile", "undefined reference to foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ev := cap.last()
	if ev.Name != "swarm:failure" {
		t.Errorf("event name = %q, want swarm:failure", ev.Name)
	}
	payload := ev.Data[0].(map[string]interface{})
	if payload["fail_type"] != "compile" {
		t.Errorf("fail_type = %v, want compile", payload["fail_type"])
	}
	if payload["summary"] != "undefined reference to foo" {
		t.Errorf("summary mismatch")
	}
	msg := payload["msg"].(string)
	if !strings.Contains(msg, "compile failure") {
		t.Errorf("msg missing failure info: %s", msg)
	}
}

func TestNotifyWaitingUser(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()

	err := n.NotifyWaitingUser(run, "Please confirm requirement X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ev := cap.last()
	if ev.Name != "swarm:waiting_user" {
		t.Errorf("event name = %q, want swarm:waiting_user", ev.Name)
	}
	payload := ev.Data[0].(map[string]interface{})
	if payload["message"] != "Please confirm requirement X" {
		t.Errorf("message mismatch")
	}
	msg := payload["msg"].(string)
	if !strings.Contains(msg, "Waiting for user input") {
		t.Errorf("msg missing waiting info: %s", msg)
	}
}

func TestNotifyRunComplete_WithReport(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()
	run.Status = SwarmStatusCompleted

	report := &SwarmReport{
		RunID:  run.ID,
		Mode:   run.Mode,
		Status: run.Status,
		Statistics: ReportStatistics{
			TotalTasks:     10,
			CompletedTasks: 8,
			FailedTasks:    2,
			TotalRounds:    3,
		},
	}

	err := n.NotifyRunComplete(run, report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ev := cap.last()
	if ev.Name != "swarm:run_complete" {
		t.Errorf("event name = %q, want swarm:run_complete", ev.Name)
	}
	payload := ev.Data[0].(map[string]interface{})
	if payload["status"] != string(SwarmStatusCompleted) {
		t.Errorf("status = %v, want completed", payload["status"])
	}
	if payload["total_tasks"] != 10 {
		t.Errorf("total_tasks = %v, want 10", payload["total_tasks"])
	}
	if payload["completed_tasks"] != 8 {
		t.Errorf("completed_tasks = %v, want 8", payload["completed_tasks"])
	}
	if payload["total_rounds"] != 3 {
		t.Errorf("total_rounds = %v, want 3", payload["total_rounds"])
	}
	msg := payload["msg"].(string)
	if !strings.Contains(msg, "completed") || !strings.Contains(msg, "8/10") {
		t.Errorf("msg missing report stats: %s", msg)
	}
}

func TestNotifyRunComplete_NilReport(t *testing.T) {
	cap := &eventCapture{}
	n := NewDefaultSwarmNotifierWithEmitter(cap.emitter())
	run := makeTestRun()
	run.Status = SwarmStatusFailed

	err := n.NotifyRunComplete(run, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := cap.last().Data[0].(map[string]interface{})
	// Should not have stats keys when report is nil
	if _, ok := payload["total_tasks"]; ok {
		t.Error("expected no total_tasks key when report is nil")
	}
}

// ---------------------------------------------------------------------------
// Unit tests – NoopSwarmNotifier
// ---------------------------------------------------------------------------

func TestNoopSwarmNotifier(t *testing.T) {
	n := &NoopSwarmNotifier{}
	run := makeTestRun()
	agent := &SwarmAgent{ID: "a1", Role: RoleDeveloper}

	if err := n.NotifyPhaseChange(run, PhaseTest); err != nil {
		t.Errorf("NotifyPhaseChange: %v", err)
	}
	if err := n.NotifyAgentComplete(run, agent); err != nil {
		t.Errorf("NotifyAgentComplete: %v", err)
	}
	if err := n.NotifyFailure(run, "test", "oops"); err != nil {
		t.Errorf("NotifyFailure: %v", err)
	}
	if err := n.NotifyWaitingUser(run, "confirm"); err != nil {
		t.Errorf("NotifyWaitingUser: %v", err)
	}
	if err := n.NotifyRunComplete(run, nil); err != nil {
		t.Errorf("NotifyRunComplete: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests – helper functions
// ---------------------------------------------------------------------------

func TestCompletedTaskCount(t *testing.T) {
	run := &SwarmRun{
		Agents: []SwarmAgent{
			{Status: "completed"},
			{Status: "running"},
			{Status: "completed"},
			{Status: "failed"},
		},
	}
	if got := completedTaskCount(run); got != 2 {
		t.Errorf("completedTaskCount = %d, want 2", got)
	}
}

func TestCompletedTaskCount_Empty(t *testing.T) {
	run := &SwarmRun{}
	if got := completedTaskCount(run); got != 0 {
		t.Errorf("completedTaskCount = %d, want 0", got)
	}
}

func TestAgentDuration_WithTimes(t *testing.T) {
	start := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 10, 5, 30, 0, time.UTC)
	agent := &SwarmAgent{StartedAt: &start, CompletedAt: &end}
	dur := agentDuration(agent)
	expected := 5*time.Minute + 30*time.Second
	if dur != expected {
		t.Errorf("agentDuration = %v, want %v", dur, expected)
	}
}

func TestAgentDuration_NoStart(t *testing.T) {
	agent := &SwarmAgent{}
	if dur := agentDuration(agent); dur != 0 {
		t.Errorf("agentDuration = %v, want 0", dur)
	}
}

// ---------------------------------------------------------------------------
// Unit tests – message formatting
// ---------------------------------------------------------------------------

func TestFormatPhaseChangeMessage(t *testing.T) {
	msg := formatPhaseChangeMessage("run-1", PhaseTest, 3, 5)
	if !strings.Contains(msg, "run-1") {
		t.Error("missing run ID")
	}
	if !strings.Contains(msg, string(PhaseTest)) {
		t.Error("missing phase")
	}
	if !strings.Contains(msg, "3/5") {
		t.Error("missing task counts")
	}
}

func TestFormatAgentCompleteMessage(t *testing.T) {
	agent := &SwarmAgent{ID: "ag-1", Role: RoleTester, TaskIndex: 7}
	msg := formatAgentCompleteMessage("run-2", agent, 2*time.Minute)
	if !strings.Contains(msg, "ag-1") || !strings.Contains(msg, "tester") {
		t.Error("missing agent info")
	}
	if !strings.Contains(msg, "task 7") {
		t.Error("missing task index")
	}
}

func TestFormatFailureMessage(t *testing.T) {
	msg := formatFailureMessage("run-3", "test", "assertion failed")
	if !strings.Contains(msg, "test failure") || !strings.Contains(msg, "assertion failed") {
		t.Error("missing failure details")
	}
}

func TestFormatRunCompleteMessage_WithReport(t *testing.T) {
	run := &SwarmRun{ID: "run-4", Status: SwarmStatusCompleted}
	report := &SwarmReport{
		Statistics: ReportStatistics{TotalTasks: 10, CompletedTasks: 9, TotalRounds: 2},
	}
	msg := formatRunCompleteMessage(run, report)
	if !strings.Contains(msg, "9/10") || !strings.Contains(msg, "rounds: 2") {
		t.Errorf("missing stats in message: %s", msg)
	}
}

func TestFormatRunCompleteMessage_NilReport(t *testing.T) {
	run := &SwarmRun{ID: "run-5", Status: SwarmStatusFailed}
	msg := formatRunCompleteMessage(run, nil)
	if !strings.Contains(msg, "failed") {
		t.Error("missing status")
	}
	if strings.Contains(msg, "tasks:") {
		t.Error("should not contain stats when report is nil")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestDefaultSwarmNotifier_ImplementsInterface(t *testing.T) {
	var _ SwarmNotifier = (*DefaultSwarmNotifier)(nil)
}

func TestNoopSwarmNotifier_ImplementsInterface(t *testing.T) {
	var _ SwarmNotifier = (*NoopSwarmNotifier)(nil)
}

// ---------------------------------------------------------------------------
// Nil-safe constructor
// ---------------------------------------------------------------------------

func TestNewDefaultSwarmNotifierWithEmitter_Nil(t *testing.T) {
	n := NewDefaultSwarmNotifierWithEmitter(nil)
	// Should not panic
	err := n.NotifyPhaseChange(makeTestRun(), PhaseTest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewDefaultSwarmNotifier_NilApp(t *testing.T) {
	n := NewDefaultSwarmNotifier(nil)
	// Should not panic
	err := n.NotifyPhaseChange(makeTestRun(), PhaseTest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
