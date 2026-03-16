package skill

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/discovery"
)

// mockSettingsRepo is a simple in-memory SystemSettingsRepository for testing.
type mockSettingsRepo struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockSettingsRepo() *mockSettingsRepo {
	return &mockSettingsRepo{data: make(map[string]string)}
}

func (m *mockSettingsRepo) Set(_ context.Context, key, valueJSON string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = valueJSON
	return nil
}

func (m *mockSettingsRepo) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}

// mockActionHandler records calls and optionally returns errors.
type mockActionHandler struct {
	mu      sync.Mutex
	calls   []actionCall
	failOn  map[string]int // action → remaining fail count
}

type actionCall struct {
	Action string
	Params map[string]interface{}
}

func newMockActionHandler() *mockActionHandler {
	return &mockActionHandler{failOn: make(map[string]int)}
}

func (h *mockActionHandler) HandleAction(_ context.Context, action string, params map[string]interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, actionCall{Action: action, Params: params})
	if count, ok := h.failOn[action]; ok && count > 0 {
		h.failOn[action] = count - 1
		return fmt.Errorf("mock error on %s", action)
	}
	return nil
}

func TestRegisterAndList(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name:        "deploy",
		Description: "Deploy project",
		Triggers:    []string{"deploy", "部署"},
		Steps: []SkillStep{
			{Action: "launch_session", Params: map[string]interface{}{"tool": "claude"}, OnError: "stop"},
		},
	}

	if err := exec.Register(context.Background(), skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	skills := exec.List(context.Background())
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "deploy" {
		t.Errorf("expected name 'deploy', got %q", skills[0].Name)
	}
	if skills[0].Status != "active" {
		t.Errorf("expected status 'active', got %q", skills[0].Status)
	}
	if skills[0].CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestRegisterEmptyName(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	err = exec.Register(context.Background(), SkillDefinition{})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestDelete(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{Name: "test-skill", Description: "test"}
	if err := exec.Register(context.Background(), skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := exec.Delete(context.Background(), "test-skill"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	skills := exec.List(context.Background())
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills after delete, got %d", len(skills))
	}
}

func TestDeleteNotFound(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	err = exec.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestGet(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	if got := exec.Get(context.Background(), "nope"); got != nil {
		t.Fatal("expected nil for nonexistent skill")
	}

	skill := SkillDefinition{Name: "my-skill", Description: "desc"}
	_ = exec.Register(context.Background(), skill)

	got := exec.Get(context.Background(), "my-skill")
	if got == nil || got.Name != "my-skill" {
		t.Fatal("expected to get registered skill")
	}
}

func TestExecuteSuccess(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name: "multi-step",
		Steps: []SkillStep{
			{Action: "step_a", Params: map[string]interface{}{"key": "val_a"}},
			{Action: "step_b", Params: map[string]interface{}{"key": "val_b"}},
		},
	}
	_ = exec.Register(context.Background(), skill)

	var progress []string
	err = exec.Execute(context.Background(), "multi-step", nil, func(step int, msg string) {
		progress = append(progress, msg)
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(handler.calls) != 2 {
		t.Fatalf("expected 2 action calls, got %d", len(handler.calls))
	}
	if handler.calls[0].Action != "step_a" || handler.calls[1].Action != "step_b" {
		t.Errorf("unexpected action sequence: %v", handler.calls)
	}
	if len(progress) != 2 {
		t.Errorf("expected 2 progress callbacks, got %d", len(progress))
	}
}

func TestExecuteNotFound(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	err = exec.Execute(context.Background(), "nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestExecuteNoHandler(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()

	exec, err := NewExecutor(repo, disc, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name:  "needs-handler",
		Steps: []SkillStep{{Action: "do_something"}},
	}
	_ = exec.Register(context.Background(), skill)

	err = exec.Execute(context.Background(), "needs-handler", nil, nil)
	if err == nil {
		t.Fatal("expected error when no handler is set")
	}
}

func TestExecuteOnErrorStop(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()
	handler.failOn["fail_step"] = 1

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name: "stop-on-error",
		Steps: []SkillStep{
			{Action: "fail_step", OnError: "stop"},
			{Action: "should_not_run"},
		},
	}
	_ = exec.Register(context.Background(), skill)

	err = exec.Execute(context.Background(), "stop-on-error", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	if len(handler.calls) != 1 {
		t.Fatalf("expected 1 call (stopped), got %d", len(handler.calls))
	}
}

func TestExecuteOnErrorSkip(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()
	handler.failOn["fail_step"] = 1

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name: "skip-on-error",
		Steps: []SkillStep{
			{Action: "fail_step", OnError: "skip"},
			{Action: "next_step"},
		},
	}
	_ = exec.Register(context.Background(), skill)

	err = exec.Execute(context.Background(), "skip-on-error", nil, nil)
	if err != nil {
		t.Fatalf("Execute should succeed with skip: %v", err)
	}

	if len(handler.calls) != 2 {
		t.Fatalf("expected 2 calls (skip + next), got %d", len(handler.calls))
	}
}

func TestExecuteOnErrorRetrySuccess(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()
	// Fail once, then succeed on retry.
	handler.failOn["retry_step"] = 1

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name: "retry-success",
		Steps: []SkillStep{
			{Action: "retry_step", OnError: "retry"},
		},
	}
	_ = exec.Register(context.Background(), skill)

	err = exec.Execute(context.Background(), "retry-success", nil, nil)
	if err != nil {
		t.Fatalf("Execute should succeed after retry: %v", err)
	}

	// First call fails, second (retry) succeeds.
	if len(handler.calls) != 2 {
		t.Fatalf("expected 2 calls (fail + retry), got %d", len(handler.calls))
	}
}

func TestExecuteOnErrorRetryFail(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()
	// Fail twice — both original and retry.
	handler.failOn["retry_step"] = 2

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name: "retry-fail",
		Steps: []SkillStep{
			{Action: "retry_step", OnError: "retry"},
			{Action: "should_not_run"},
		},
	}
	_ = exec.Register(context.Background(), skill)

	err = exec.Execute(context.Background(), "retry-fail", nil, nil)
	if err == nil {
		t.Fatal("expected error after retry failure")
	}

	// Original + retry = 2 calls, third step should not run.
	if len(handler.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(handler.calls))
	}
}

func TestExecuteParamsMerge(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name: "merge-params",
		Steps: []SkillStep{
			{Action: "action", Params: map[string]interface{}{"step_key": "step_val", "shared": "from_step"}},
		},
	}
	_ = exec.Register(context.Background(), skill)

	baseParams := map[string]interface{}{"base_key": "base_val", "shared": "from_base"}
	err = exec.Execute(context.Background(), "merge-params", baseParams, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	call := handler.calls[0]
	if call.Params["base_key"] != "base_val" {
		t.Error("expected base_key from base params")
	}
	if call.Params["step_key"] != "step_val" {
		t.Error("expected step_key from step params")
	}
	// Step params should override base params.
	if call.Params["shared"] != "from_step" {
		t.Errorf("expected step params to override base, got %v", call.Params["shared"])
	}
}

func TestPersistenceAcrossInstances(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()
	handler := newMockActionHandler()

	exec1, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name:        "persistent",
		Description: "persisted skill",
		Triggers:    []string{"persist"},
		Steps:       []SkillStep{{Action: "do"}},
		CreatedAt:   time.Now(),
	}
	_ = exec1.Register(context.Background(), skill)

	// Create a new executor from the same repo — should load the skill.
	exec2, err := NewExecutor(repo, disc, handler)
	if err != nil {
		t.Fatalf("NewExecutor (2nd): %v", err)
	}

	skills := exec2.List(context.Background())
	if len(skills) != 1 || skills[0].Name != "persistent" {
		t.Fatalf("expected persisted skill, got %v", skills)
	}
}

func TestSetActionHandler(t *testing.T) {
	repo := newMockSettingsRepo()
	disc := newTestProtocol()

	exec, err := NewExecutor(repo, disc, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	skill := SkillDefinition{
		Name:  "late-handler",
		Steps: []SkillStep{{Action: "action"}},
	}
	_ = exec.Register(context.Background(), skill)

	// Should fail without handler.
	err = exec.Execute(context.Background(), "late-handler", nil, nil)
	if err == nil {
		t.Fatal("expected error without handler")
	}

	// Set handler and retry.
	handler := newMockActionHandler()
	exec.SetActionHandler(handler)

	err = exec.Execute(context.Background(), "late-handler", nil, nil)
	if err != nil {
		t.Fatalf("Execute after SetActionHandler: %v", err)
	}
	if len(handler.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(handler.calls))
	}
}

// newTestProtocol creates a discovery.Protocol for testing.
func newTestProtocol() *discovery.Protocol {
	return discovery.NewProtocol()
}
