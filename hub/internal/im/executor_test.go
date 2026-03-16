package im

import (
	"context"
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/nlrouter"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockSessionManager struct {
	sessions   []SessionInfo
	createErr  error
	writeErr   error
	getErr     error
	interErr   error
	killErr    error
	createdID  string
}

func (m *mockSessionManager) CreateSession(_ context.Context, _, tool, _, _ string) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	return m.createdID, nil
}

func (m *mockSessionManager) WriteInput(_ context.Context, _, _ string) error {
	return m.writeErr
}

func (m *mockSessionManager) ListSessions(_ context.Context, _ string) ([]SessionInfo, error) {
	return m.sessions, nil
}

func (m *mockSessionManager) GetSession(_ context.Context, id string) (*SessionInfo, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, s := range m.sessions {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("session %s not found", id)
}

func (m *mockSessionManager) InterruptSession(_ context.Context, _ string) error {
	return m.interErr
}

func (m *mockSessionManager) KillSession(_ context.Context, _ string) error {
	return m.killErr
}

type mockDeviceManager struct {
	machines []MachineInfo
	listErr  error
}

func (m *mockDeviceManager) ListMachines(_ context.Context, _ string) ([]MachineInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.machines, nil
}

func (m *mockDeviceManager) SendToMachine(_ string, _ any) error { return nil }
func (m *mockDeviceManager) IsMachineOnline(_ string) bool       { return true }

type mockScreenshotService struct {
	imageKey string
	err      error
}

func (m *mockScreenshotService) RequestScreenshot(_ context.Context, _ string) (string, error) {
	return m.imageKey, m.err
}

type mockMCPRegistry struct {
	result interface{}
	err    error
}

func (m *mockMCPRegistry) CallTool(_ context.Context, _, _ string, _ map[string]interface{}) (interface{}, error) {
	return m.result, m.err
}

func (m *mockMCPRegistry) ListServers(_ context.Context) []MCPServerInfo { return nil }

type mockSkillExecutor struct {
	skills []SkillInfo
	err    error
}

func (m *mockSkillExecutor) Execute(_ context.Context, _ string, _ map[string]interface{}, fn func(int, string)) error {
	if fn != nil {
		fn(0, "步骤 1 完成")
	}
	return m.err
}

func (m *mockSkillExecutor) List(_ context.Context) []SkillInfo { return m.skills }

type mockCrystallizer struct {
	candidate *SkillCandidate
	err       error
}

func (m *mockCrystallizer) CrystallizeFromContext(_ context.Context, _ string) (*SkillCandidate, error) {
	return m.candidate, m.err
}

type mockMemoryStore struct {
	data     *MemoryData
	getErr   error
	clearErr error
}

func (m *mockMemoryStore) Get(_ context.Context, _ string) (*MemoryData, error) {
	return m.data, m.getErr
}

func (m *mockMemoryStore) Clear(_ context.Context, _ string) error {
	return m.clearErr
}

func (m *mockMemoryStore) RecordAction(_ context.Context, _ string, _ MemoryEntry) error {
	return nil
}

type mockContextManager struct {
	data *ContextWindowData
}

func (m *mockContextManager) Get(_ string) *ContextWindowData {
	if m.data == nil {
		return &ContextWindowData{}
	}
	return m.data
}

type mockToolCatalog struct {
	available map[string]bool
}

func (m *mockToolCatalog) IsToolAvailable(name string) bool {
	return m.available[name]
}

func (m *mockToolCatalog) ListAvailableTools() []string {
	var tools []string
	for k, v := range m.available {
		if v {
			tools = append(tools, k)
		}
	}
	return tools
}

// ---------------------------------------------------------------------------
// Helper to build a BridgeExecutor with defaults
// ---------------------------------------------------------------------------

func newTestExecutor() (*BridgeExecutor, *mockSessionManager, *mockContextManager) {
	sm := &mockSessionManager{
		createdID: "sess_001",
		sessions: []SessionInfo{
			{ID: "sess_001", Tool: "claude", Title: "Test Session", Status: "running"},
		},
	}
	cm := &mockContextManager{data: &ContextWindowData{}}
	b := NewBridgeExecutor(
		sm,
		&mockDeviceManager{machines: []MachineInfo{
			{ID: "m1", Name: "MacBook", Platform: "darwin", Online: true},
		}},
		&mockScreenshotService{imageKey: "img_key_123"},
		&mockMCPRegistry{result: "ok"},
		&mockSkillExecutor{skills: []SkillInfo{
			{Name: "deploy", Description: "Deploy project", Triggers: []string{"部署"}, Status: "active"},
		}},
		&mockCrystallizer{candidate: &SkillCandidate{
			Name: "auto-skill", Description: "Auto generated", Steps: []string{"step1"},
		}},
		&mockMemoryStore{data: &MemoryData{
			DefaultTool:   "claude",
			RecentCount:   42,
			Preferences:   map[string]string{"theme": "dark"},
			PatternCounts: map[string]int{"launch_session:claude": 5},
		}},
		cm,
		&mockToolCatalog{available: map[string]bool{"claude": true, "codex": true}},
	)
	return b, sm, cm
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestExecute_LaunchSession_Success(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentLaunchSession,
		Params: map[string]interface{}{"tool": "claude"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.StatusIcon != "🚀" {
		t.Errorf("expected 🚀 icon, got %s", resp.StatusIcon)
	}
}

func TestExecute_LaunchSession_MissingTool(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentLaunchSession,
		Params: map[string]interface{}{},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestExecute_LaunchSession_ToolUnavailable(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentLaunchSession,
		Params: map[string]interface{}{"tool": "nonexistent"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestExecute_SendInput_Success(t *testing.T) {
	b, _, cm := newTestExecutor()
	cm.data.ActiveSession = "sess_001"
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentSendInput,
		Params: map[string]interface{}{"text": "hello world"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_SendInput_NoSession(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentSendInput,
		Params: map[string]interface{}{"text": "hello"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestExecute_ListMachines(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{Name: nlrouter.IntentListMachines, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(resp.Fields) != 1 {
		t.Errorf("expected 1 field, got %d", len(resp.Fields))
	}
}

func TestExecute_ListSessions(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{Name: nlrouter.IntentListSessions, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(resp.Fields) != 1 {
		t.Errorf("expected 1 session field, got %d", len(resp.Fields))
	}
}

func TestExecute_SessionDetail(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentSessionDetail,
		Params: map[string]interface{}{"session_id": "sess_001"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_UseSession(t *testing.T) {
	b, _, cm := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentUseSession,
		Params: map[string]interface{}{"session_id": "sess_001"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if cm.data.ActiveSession != "sess_001" {
		t.Errorf("expected active session to be set, got %s", cm.data.ActiveSession)
	}
}

func TestExecute_ExitSession(t *testing.T) {
	b, _, cm := newTestExecutor()
	cm.data.ActiveSession = "sess_001"
	intent := &nlrouter.Intent{Name: nlrouter.IntentExitSession, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if cm.data.ActiveSession != "" {
		t.Errorf("expected active session to be cleared")
	}
}

func TestExecute_InterruptSession(t *testing.T) {
	b, _, cm := newTestExecutor()
	cm.data.ActiveSession = "sess_001"
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentInterruptSession,
		Params: map[string]interface{}{},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_KillSession(t *testing.T) {
	b, _, cm := newTestExecutor()
	cm.data.ActiveSession = "sess_001"
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentKillSession,
		Params: map[string]interface{}{},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if cm.data.ActiveSession != "" {
		t.Errorf("expected active session to be cleared after kill")
	}
}

func TestExecute_Screenshot(t *testing.T) {
	b, _, cm := newTestExecutor()
	cm.data.ActiveSession = "sess_001"
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentScreenshot,
		Params: map[string]interface{}{},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.StatusIcon != "📸" {
		t.Errorf("expected 📸 icon, got %s", resp.StatusIcon)
	}
}

func TestExecute_CallMCPTool(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name: nlrouter.IntentCallMCPTool,
		Params: map[string]interface{}{
			"server_id": "srv1",
			"tool_name": "search",
			"input":     map[string]interface{}{"query": "test"},
		},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_RunSkill(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentRunSkill,
		Params: map[string]interface{}{"skill_name": "deploy"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_RunSkill_ListWhenNoName(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentRunSkill,
		Params: map[string]interface{}{},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(resp.Fields) != 1 {
		t.Errorf("expected 1 skill field, got %d", len(resp.Fields))
	}
}

func TestExecute_ViewMemory(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{Name: nlrouter.IntentViewMemory, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.StatusIcon != "🧠" {
		t.Errorf("expected 🧠 icon, got %s", resp.StatusIcon)
	}
}

func TestExecute_ClearMemory(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{Name: nlrouter.IntentClearMemory, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_CrystallizeSkill(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{Name: nlrouter.IntentCrystallizeSkill, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.StatusIcon != "💎" {
		t.Errorf("expected 💎 icon, got %s", resp.StatusIcon)
	}
}

func TestExecute_Help(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{Name: nlrouter.IntentHelp, Params: map[string]interface{}{}}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExecute_Unknown_WithCandidates(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentUnknown,
		Params: map[string]interface{}{},
		Candidates: []nlrouter.Intent{
			{Name: nlrouter.IntentListMachines},
			{Name: nlrouter.IntentListSessions},
		},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.StatusIcon != "🤔" {
		t.Errorf("expected 🤔 icon, got %s", resp.StatusIcon)
	}
}

func TestExecute_FuzzySessionMatch(t *testing.T) {
	b, _, _ := newTestExecutor()
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentSessionDetail,
		Params: map[string]interface{}{"session_ref": "claude"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should match the "claude" session via fuzzy matching.
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d (body: %s)", resp.StatusCode, resp.Body)
	}
}

func TestExecute_LaunchSession_CreateError(t *testing.T) {
	b, sm, _ := newTestExecutor()
	sm.createErr = fmt.Errorf("device offline")
	intent := &nlrouter.Intent{
		Name:   nlrouter.IntentLaunchSession,
		Params: map[string]interface{}{"tool": "claude"},
	}
	resp, err := b.Execute(context.Background(), "user1", intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}
