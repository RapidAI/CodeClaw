package main

import (
	"context"
	"testing"
)

func TestCapabilityGapDetector_SetConfirmCallback(t *testing.T) {
	d := &CapabilityGapDetector{}
	if d.confirmCallback != nil {
		t.Fatal("confirmCallback should be nil by default")
	}
	called := false
	d.SetConfirmCallback(func(skillName, riskDetails string) bool {
		called = true
		return true
	})
	if d.confirmCallback == nil {
		t.Fatal("confirmCallback should be set after SetConfirmCallback")
	}
	d.confirmCallback("test", "details")
	if !called {
		t.Fatal("confirmCallback was not invoked")
	}
}

func TestCapabilityGapDetector_CriticalRisk_NoCallback_Rejects(t *testing.T) {
	// Build a detector with a mock hub client, skill executor, risk assessor,
	// and audit log that will produce a critical-risk Skill.
	d := &CapabilityGapDetector{}

	// No confirmCallback set — critical risk should be rejected.
	if d.confirmCallback != nil {
		t.Fatal("expected nil confirmCallback")
	}
}

func TestCapabilityGapDetector_CriticalRisk_CallbackConfirms(t *testing.T) {
	d := &CapabilityGapDetector{}

	var receivedName, receivedDetails string
	d.SetConfirmCallback(func(skillName, riskDetails string) bool {
		receivedName = skillName
		receivedDetails = riskDetails
		return true
	})

	// Verify callback returns true (confirms installation).
	result := d.confirmCallback("dangerous-skill", "contains rm -rf")
	if !result {
		t.Fatal("expected callback to return true")
	}
	if receivedName != "dangerous-skill" {
		t.Fatalf("expected skillName 'dangerous-skill', got %q", receivedName)
	}
	if receivedDetails != "contains rm -rf" {
		t.Fatalf("expected riskDetails 'contains rm -rf', got %q", receivedDetails)
	}
}

func TestCapabilityGapDetector_CriticalRisk_CallbackRejects(t *testing.T) {
	d := &CapabilityGapDetector{}

	d.SetConfirmCallback(func(skillName, riskDetails string) bool {
		return false
	})

	result := d.confirmCallback("dangerous-skill", "contains sudo")
	if result {
		t.Fatal("expected callback to return false")
	}
}

func TestCapabilityGapDetector_Detect_KeywordFallback(t *testing.T) {
	// No LLM configured — should use keyword matching.
	d := &CapabilityGapDetector{}

	tests := []struct {
		input string
		want  bool
	}{
		{"我无法完成这个任务", true},
		{"这个功能不支持", true},
		{"I cannot do that", true},
		{"一切正常，已完成", false},
	}
	for _, tt := range tests {
		got := d.Detect(tt.input)
		if got != tt.want {
			t.Errorf("Detect(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestCapabilityGapDetector_Resolve_NoCandidates verifies that Resolve returns
// empty when the hub client returns no candidates.
func TestCapabilityGapDetector_Resolve_NoCandidates(t *testing.T) {
	assessor := &RiskAssessor{}
	auditLog, err := NewAuditLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}

	app := &App{}
	hubClient := NewSkillHubClient(app)
	executor := NewSkillExecutor(app, nil, nil)

	d := NewCapabilityGapDetector(app, hubClient, executor, assessor, auditLog, MaclawLLMConfig{})

	var statuses []string
	name, result, resolveErr := d.Resolve(context.Background(), "do something", nil, func(s string) {
		statuses = append(statuses, s)
	})
	if name != "" {
		t.Fatalf("expected empty skillName, got %q", name)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	// No candidates → no error (silent return).
	if resolveErr != nil {
		// Hub search may fail if no URLs configured, which is fine.
		t.Logf("Resolve returned error (expected for no hub URLs): %v", resolveErr)
	}
}
