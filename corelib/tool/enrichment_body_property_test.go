package tool

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: skillrouter-body-aware-retrieval, Property 6: Enrichment Prompt 包含 Body Summary
// For any non-empty bodySummary, user prompt contains bodySummary.
// For empty bodySummary, user prompt does not contain body-related content.
// **Validates: Requirements 5.1, 5.2**
func TestProperty_EnrichmentPromptContainsBodySummary(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		toolName := rapid.StringMatching(`[a-z_]{3,20}`).Draw(t, "toolName")
		description := rapid.StringMatching(`[a-zA-Z ]{5,50}`).Draw(t, "description")
		bodySummary := rapid.StringMatching(`[a-zA-Z0-9 \-\n]{10,200}`).Draw(t, "bodySummary")

		_, user := GenerateEnrichmentPrompt(toolName, description, bodySummary)

		if !strings.Contains(user, bodySummary) {
			t.Fatalf("user prompt should contain bodySummary %q, got: %s", bodySummary, user)
		}
		if !strings.Contains(user, toolName) {
			t.Fatalf("user prompt should contain toolName %q", toolName)
		}
	})
}

func TestProperty_EnrichmentPromptEmptyBody(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		toolName := rapid.StringMatching(`[a-z_]{3,20}`).Draw(t, "toolName")
		description := rapid.StringMatching(`[a-zA-Z ]{5,50}`).Draw(t, "description")

		sys, user := GenerateEnrichmentPrompt(toolName, description, "")

		if strings.Contains(user, "Body Summary") {
			t.Fatalf("user prompt should not contain 'Body Summary' when bodySummary is empty, got: %s", user)
		}
		if strings.Contains(sys, "body summary") {
			t.Fatalf("system prompt should not mention 'body summary' when bodySummary is empty, got: %s", sys)
		}
		if !strings.Contains(user, toolName) {
			t.Fatalf("user prompt should contain toolName %q", toolName)
		}
	})
}
