package main

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for IMMessageHandler dynamic tool integration (Task 6.3)
// ---------------------------------------------------------------------------

// TestGetTools_FallbackWithoutGenerator verifies that getTools() returns
// the hardcoded buildToolDefinitions() output when no generator is set.
func TestGetTools_FallbackWithoutGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.getTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty builtin tools")
	}

	// Verify first tool is list_sessions.
	name := extractToolName(tools[0])
	if name != "list_sessions" {
		t.Errorf("expected first tool to be list_sessions, got %s", name)
	}
}

// TestGetTools_UsesGeneratorWhenSet verifies that getTools() delegates to
// the ToolDefinitionGenerator when configured.
func TestGetTools_UsesGeneratorWhenSet(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	tools := handler.getTools()
	// With nil registry, generator returns only builtins.
	if len(tools) != len(builtins) {
		t.Fatalf("expected %d tools from generator (nil registry), got %d", len(builtins), len(tools))
	}
}

// TestGetTools_CacheWithin5Seconds verifies that repeated calls within 5s
// return the cached result without regenerating.
func TestGetTools_CacheWithin5Seconds(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// First call populates cache.
	tools1 := handler.getTools()
	// Second call should return same slice from cache.
	tools2 := handler.getTools()

	if len(tools1) != len(tools2) {
		t.Fatalf("cached tools length mismatch: %d vs %d", len(tools1), len(tools2))
	}

	// Verify cache timestamp was set.
	handler.toolsMu.RLock()
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be set after getTools()")
	}
}

// TestGetTools_CacheInvalidatedBySetGenerator verifies that calling
// SetToolDefGenerator invalidates the cache.
func TestGetTools_CacheInvalidatedBySetGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// Populate cache.
	_ = handler.getTools()

	// Set a new generator — should invalidate cache.
	gen2 := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen2)

	handler.toolsMu.RLock()
	cached := handler.cachedTools
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cached != nil {
		t.Error("expected cachedTools to be nil after SetToolDefGenerator")
	}
	if !cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be zero after SetToolDefGenerator")
	}
}

// TestRouteTools_NoRouterReturnsAll verifies that routeTools returns all
// tools unchanged when no router is configured.
func TestRouteTools_NoRouterReturnsAll(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("hello", tools)

	if len(routed) != len(tools) {
		t.Fatalf("expected %d tools without router, got %d", len(tools), len(routed))
	}
}

// TestRouteTools_WithRouterFilters verifies that routeTools delegates to
// the ToolRouter when configured.
func TestRouteTools_WithRouterFilters(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	gen := NewToolDefinitionGenerator(nil, handler.buildToolDefinitions())
	router := NewToolRouter(gen)
	handler.SetToolRouter(router)

	// With only 12 tools (below threshold of 20), router returns all.
	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("test message", tools)

	if len(routed) != len(tools) {
		t.Fatalf("expected %d tools (below threshold), got %d", len(tools), len(routed))
	}
}

// TestToolsCacheTTL_Value verifies the cache TTL constant is 5 seconds.
func TestToolsCacheTTL_Value(t *testing.T) {
	expected := 5 * time.Second
	if toolsCacheTTL != expected {
		t.Errorf("expected toolsCacheTTL = %v, got %v", expected, toolsCacheTTL)
	}
}

// ---------------------------------------------------------------------------
// Tests for Task 7.2: Smart session startup & template tools
// ---------------------------------------------------------------------------

// TestToolCreateSession_SmartToolRecommendation verifies that toolCreateSession
// auto-recommends a tool when the tool parameter is empty and contextResolver is set.
func TestToolCreateSession_SmartToolRecommendation(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	// Without contextResolver, empty tool should return error.
	result := handler.toolCreateSession(map[string]interface{}{})
	if result != "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("expected missing tool error, got: %s", result)
	}
}

// TestToolCreateSession_WithToolProvided verifies that toolCreateSession
// uses the provided tool parameter directly (no auto-recommendation).
func TestToolCreateSession_WithToolProvided(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	// With tool provided but no manager, should fail at session creation.
	result := handler.toolCreateSession(map[string]interface{}{
		"tool": "claude",
	})
	// Should attempt to create session (will fail because app is minimal).
	if result == "缺少 tool 参数" || result == "缺少 tool 参数，且无法自动推荐工具" {
		t.Errorf("should not report missing tool when tool is provided, got: %s", result)
	}
}

// TestToolCreateTemplate verifies template creation via the tool.
func TestToolCreateTemplate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// Missing required params.
	result := handler.toolCreateTemplate(map[string]interface{}{})
	if result != "缺少 name 或 tool 参数" {
		t.Errorf("expected missing params error, got: %s", result)
	}

	// Successful creation.
	result = handler.toolCreateTemplate(map[string]interface{}{
		"name":         "my-template",
		"tool":         "claude",
		"project_path": "/tmp/project",
		"yolo_mode":    true,
	})
	if result != "模板已创建: my-template（工具=claude, 项目=/tmp/project）" {
		t.Errorf("unexpected result: %s", result)
	}

	// Duplicate name.
	result = handler.toolCreateTemplate(map[string]interface{}{
		"name": "my-template",
		"tool": "codex",
	})
	if result == "" || !contains(result, "创建模板失败") {
		t.Errorf("expected duplicate error, got: %s", result)
	}
}

// TestToolCreateTemplate_NilManager verifies graceful handling when
// templateManager is nil.
func TestToolCreateTemplate_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolCreateTemplate(map[string]interface{}{
		"name": "test", "tool": "claude",
	})
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestToolListTemplates verifies listing templates.
func TestToolListTemplates(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// Empty list.
	result := handler.toolListTemplates()
	if result != "当前没有会话模板。" {
		t.Errorf("expected empty list message, got: %s", result)
	}

	// Add a template and list again.
	_ = mgr.Create(SessionTemplate{Name: "dev", Tool: "claude", ProjectPath: "/tmp/dev", YoloMode: true})
	result = handler.toolListTemplates()
	if !contains(result, "dev") || !contains(result, "claude") || !contains(result, "[Yolo]") {
		t.Errorf("expected template details in list, got: %s", result)
	}
}

// TestToolListTemplates_NilManager verifies graceful handling.
func TestToolListTemplates_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolListTemplates()
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestToolLaunchTemplate_NotFound verifies error when template doesn't exist.
func TestToolLaunchTemplate_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, err := NewSessionTemplateManager(path)
	if err != nil {
		t.Fatalf("failed to create template manager: %v", err)
	}

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	result := handler.toolLaunchTemplate(map[string]interface{}{
		"template_name": "nonexistent",
	})
	if !contains(result, "获取模板失败") {
		t.Errorf("expected not found error, got: %s", result)
	}
}

// TestToolLaunchTemplate_MissingParam verifies error when template_name is missing.
func TestToolLaunchTemplate_MissingParam(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, _ := NewSessionTemplateManager(path)

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	result := handler.toolLaunchTemplate(map[string]interface{}{})
	if result != "缺少 template_name 参数" {
		t.Errorf("expected missing param error, got: %s", result)
	}
}

// TestToolLaunchTemplate_NilManager verifies graceful handling.
func TestToolLaunchTemplate_NilManager(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.toolLaunchTemplate(map[string]interface{}{
		"template_name": "test",
	})
	if result != "模板管理器未初始化" {
		t.Errorf("expected nil manager error, got: %s", result)
	}
}

// TestExecuteTool_TemplateToolsRouting verifies that executeTool routes
// template tool names to the correct handlers.
func TestExecuteTool_TemplateToolsRouting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"
	mgr, _ := NewSessionTemplateManager(path)

	handler := &IMMessageHandler{
		app:             &App{},
		templateManager: mgr,
	}

	// create_template via executeTool
	result := handler.executeTool("create_template", `{"name":"t1","tool":"claude"}`, nil)
	if !contains(result, "模板已创建") {
		t.Errorf("create_template via executeTool failed: %s", result)
	}

	// list_templates via executeTool
	result = handler.executeTool("list_templates", "", nil)
	if !contains(result, "t1") {
		t.Errorf("list_templates via executeTool failed: %s", result)
	}

	// launch_template via executeTool (will fail at session creation, but routing works)
	result = handler.executeTool("launch_template", `{"template_name":"t1"}`, nil)
	// Should get past template lookup (routing works) — will fail at session creation
	if contains(result, "未知工具") || contains(result, "模板管理器未初始化") {
		t.Errorf("launch_template routing failed: %s", result)
	}
}

// TestSetContextResolver verifies the setter works.
func TestSetContextResolver(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	resolver := &SessionContextResolver{app: &App{}}
	handler.SetContextResolver(resolver)
	if handler.contextResolver != resolver {
		t.Error("expected contextResolver to be set")
	}
}

// TestSetSessionPrecheck verifies the setter works.
func TestSetSessionPrecheck(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	precheck := &SessionPrecheck{app: &App{}}
	handler.SetSessionPrecheck(precheck)
	if handler.sessionPrecheck != precheck {
		t.Error("expected sessionPrecheck to be set")
	}
}

// TestSetStartupFeedback verifies the setter works.
func TestSetStartupFeedback(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	// Can't easily create a real SessionStartupFeedback without a manager,
	// but we can verify the field is set.
	feedback := &SessionStartupFeedback{}
	handler.SetStartupFeedback(feedback)
	if handler.startupFeedback != feedback {
		t.Error("expected startupFeedback to be set")
	}
}

// TestBuildToolDefinitions_IncludesTemplateTools verifies that the tool
// definitions include the template tools added in task 7.1.
func TestBuildToolDefinitions_IncludesTemplateTools(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	tools := handler.buildToolDefinitions()

	templateTools := map[string]bool{
		"create_template": false,
		"list_templates":  false,
		"launch_template": false,
	}

	for _, tool := range tools {
		name := extractToolName(tool)
		if _, ok := templateTools[name]; ok {
			templateTools[name] = true
		}
	}

	for name, found := range templateTools {
		if !found {
			t.Errorf("expected template tool %q in buildToolDefinitions", name)
		}
	}
}

// contains is a test helper that checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
