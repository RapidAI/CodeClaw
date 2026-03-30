package tool

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: skillrouter-body-aware-retrieval, Property 7: Builtin Body 自动填充
// For any tool name in BuiltinBodies, registering with empty Body results in
// Body == BuiltinBodies[name] and BodySummary == TruncateBody(BuiltinBodies[name], DefaultBodyMaxChars).
// **Validates: Requirements 4.2, 4.3**
func TestProperty_BuiltinBodyAutoPopulation(t *testing.T) {
	// Collect all builtin body names for sampling.
	names := make([]string, 0, len(BuiltinBodies))
	for name := range BuiltinBodies {
		names = append(names, name)
	}
	if len(names) == 0 {
		t.Skip("BuiltinBodies is empty")
	}

	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(names)-1).Draw(t, "nameIdx")
		name := names[idx]
		expectedBody := BuiltinBodies[name]
		expectedSummary := TruncateBody(expectedBody, DefaultBodyMaxChars)

		reg := NewRegistry()
		err := reg.Register(RegisteredTool{
			Name:        name,
			Description: "test tool",
			Category:    CategoryBuiltin,
		})
		if err != nil {
			t.Fatalf("register error: %v", err)
		}

		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("tool %q not found after registration", name)
		}
		if tool.Body != expectedBody {
			t.Fatalf("Body mismatch for %q:\n  got:  %q\n  want: %q", name, tool.Body, expectedBody)
		}
		if tool.BodySummary != expectedSummary {
			t.Fatalf("BodySummary mismatch for %q:\n  got:  %q\n  want: %q", name, tool.BodySummary, expectedSummary)
		}
	})
}

// Verify that registering a tool with a pre-set Body does NOT overwrite it from BuiltinBodies.
func TestProperty_BuiltinBodyNoOverwrite(t *testing.T) {
	names := make([]string, 0, len(BuiltinBodies))
	for name := range BuiltinBodies {
		names = append(names, name)
	}
	if len(names) == 0 {
		t.Skip("BuiltinBodies is empty")
	}

	rapid.Check(t, func(t *rapid.T) {
		idx := rapid.IntRange(0, len(names)-1).Draw(t, "nameIdx")
		name := names[idx]
		customBody := "custom body content for " + name

		reg := NewRegistry()
		err := reg.Register(RegisteredTool{
			Name:        name,
			Description: "test tool",
			Category:    CategoryBuiltin,
			Body:        customBody,
		})
		if err != nil {
			t.Fatalf("register error: %v", err)
		}

		tool, _ := reg.Get(name)
		if tool.Body != customBody {
			t.Fatalf("Body should not be overwritten: got %q, want %q", tool.Body, customBody)
		}
	})
}
