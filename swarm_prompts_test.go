package main

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestRenderPrompt_AllRoles(t *testing.T) {
	ctx := PromptContext{
		ProjectName:   "TestProject",
		TechStack:     "Go + React",
		TaskDesc:      "Implement user auth",
		ArchDesign:    "MVC architecture",
		InterfaceDefs: "func Login()",
		CompileErrors: "undefined: foo",
		TestCommand:   "go test ./...",
		Requirements:  "User login feature",
		FeatureList:   "Login, Register",
		ProjectStruct: "cmd/ pkg/ internal/",
		APIList:       "POST /login",
		ChangeLog:     "v1.0: initial release",
	}

	roles := []AgentRole{RoleArchitect, RoleDesigner, RoleDeveloper, RoleCompiler, RoleTester, RoleDocumenter}
	for _, role := range roles {
		result, err := RenderPrompt(role, ctx)
		if err != nil {
			t.Errorf("RenderPrompt(%s): %v", role, err)
			continue
		}
		if result == "" {
			t.Errorf("RenderPrompt(%s) returned empty string", role)
		}
	}
}

func TestRenderPrompt_UnknownRole(t *testing.T) {
	_, err := RenderPrompt("unknown", PromptContext{})
	if err == nil {
		t.Error("expected error for unknown role")
	}
}

// Feature: swarm-orchestrator, Property 11: 角色 Prompt 包含必要内容
// For any AgentRole and PromptContext, the rendered prompt must contain the
// role-specific required fields.
func TestProperty_PromptContainsRequiredFields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := PromptContext{
			ProjectName:   rapid.StringMatching(`[A-Za-z]{3,15}`).Draw(t, "project"),
			TechStack:     rapid.StringMatching(`[A-Za-z+ ]{3,20}`).Draw(t, "stack"),
			TaskDesc:      rapid.StringMatching(`[A-Za-z ]{5,30}`).Draw(t, "task"),
			ArchDesign:    rapid.StringMatching(`[A-Za-z ]{5,30}`).Draw(t, "arch"),
			InterfaceDefs: rapid.StringMatching(`[A-Za-z() ]{5,20}`).Draw(t, "iface"),
			CompileErrors: rapid.StringMatching(`[A-Za-z: ]{5,30}`).Draw(t, "errors"),
			TestCommand:   rapid.StringMatching(`[a-z ./-]{3,20}`).Draw(t, "testcmd"),
			Requirements:  rapid.StringMatching(`[A-Za-z ]{5,30}`).Draw(t, "reqs"),
			FeatureList:   rapid.StringMatching(`[A-Za-z, ]{5,20}`).Draw(t, "features"),
			ProjectStruct: rapid.StringMatching(`[a-z/ ]{5,20}`).Draw(t, "struct"),
			APIList:       rapid.StringMatching(`[A-Z /a-z]{5,20}`).Draw(t, "api"),
			ChangeLog:     rapid.StringMatching(`[A-Za-z0-9.: ]{5,20}`).Draw(t, "changelog"),
		}

		// Define required fields per role
		roleRequirements := map[AgentRole][]string{
			RoleArchitect:  {"Requirements", "TechStack"},
			RoleDeveloper:  {"TaskDesc", "ArchDesign"},
			RoleCompiler:   {"TechStack"},
			RoleTester:     {"TestCommand", "Requirements"},
			RoleDocumenter: {"ProjectStruct", "APIList"},
		}

		fieldValues := map[string]string{
			"Requirements":  ctx.Requirements,
			"TechStack":     ctx.TechStack,
			"TaskDesc":      ctx.TaskDesc,
			"ArchDesign":    ctx.ArchDesign,
			"InterfaceDefs": ctx.InterfaceDefs,
			"CompileErrors": ctx.CompileErrors,
			"TestCommand":   ctx.TestCommand,
			"FeatureList":   ctx.FeatureList,
			"ProjectStruct": ctx.ProjectStruct,
			"APIList":       ctx.APIList,
			"ChangeLog":     ctx.ChangeLog,
		}

		for role, requiredFields := range roleRequirements {
			result, err := RenderPrompt(role, ctx)
			if err != nil {
				t.Fatalf("RenderPrompt(%s): %v", role, err)
			}

			for _, field := range requiredFields {
				val := fieldValues[field]
				if !strings.Contains(result, val) {
					t.Fatalf("role %s prompt missing %s value %q", role, field, val)
				}
			}
		}
	})
}
