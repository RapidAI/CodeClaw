package main

import (
	"encoding/json"
	"testing"
)

// ── YOLO mode override tests (Task 14.2, Req 7.8) ──────────────────────────

func TestBuildToolArgs_YoloDisabledByPolicy(t *testing.T) {
	// Validates: Req 7.8 — yolo_mode_allowed=false forces YOLO off
	tuiSecurityPolicy.mu.Lock()
	tuiSecurityPolicy.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{YoloModeAllowed: false},
	}
	tuiSecurityPolicy.mu.Unlock()
	defer func() {
		tuiSecurityPolicy.mu.Lock()
		tuiSecurityPolicy.policy = nil
		tuiSecurityPolicy.mu.Unlock()
	}()

	args := buildToolArgs("claude", "/tmp/project", true, false)
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			t.Error("expected YOLO flag to be stripped when policy forbids it")
		}
	}
}

func TestBuildToolArgs_YoloAllowedByPolicy(t *testing.T) {
	// Validates: YOLO mode works normally when policy allows it
	tuiSecurityPolicy.mu.Lock()
	tuiSecurityPolicy.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{YoloModeAllowed: true},
	}
	tuiSecurityPolicy.mu.Unlock()
	defer func() {
		tuiSecurityPolicy.mu.Lock()
		tuiSecurityPolicy.policy = nil
		tuiSecurityPolicy.mu.Unlock()
	}()

	args := buildToolArgs("claude", "/tmp/project", true, false)
	found := false
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			found = true
		}
	}
	if !found {
		t.Error("expected YOLO flag to be present when policy allows it")
	}
}

func TestBuildToolArgs_YoloAllowedWhenNoCentralized(t *testing.T) {
	// Validates: YOLO mode works when centralized security is off
	tuiSecurityPolicy.mu.Lock()
	tuiSecurityPolicy.policy = nil
	tuiSecurityPolicy.mu.Unlock()

	args := buildToolArgs("claude", "/tmp/project", true, false)
	found := false
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			found = true
		}
	}
	if !found {
		t.Error("expected YOLO flag when no centralized policy")
	}
}

func TestBuildToolArgs_NoYoloRequestedNoChange(t *testing.T) {
	// When yoloMode=false, policy doesn't matter
	tuiSecurityPolicy.mu.Lock()
	tuiSecurityPolicy.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{YoloModeAllowed: false},
	}
	tuiSecurityPolicy.mu.Unlock()
	defer func() {
		tuiSecurityPolicy.mu.Lock()
		tuiSecurityPolicy.policy = nil
		tuiSecurityPolicy.mu.Unlock()
	}()

	args := buildToolArgs("claude", "/tmp/project", false, false)
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			t.Error("YOLO flag should not appear when not requested")
		}
	}
}

// ── ApplyPolicy logging tests (Task 14.2, Req 7.5, 7.6, 7.7) ──────────────

func TestApplyPolicy_NoOpWhenNilPolicy(t *testing.T) {
	cache := &TUISecurityPolicyCache{}
	// Should not panic
	cache.ApplyPolicy()
}

func TestApplyPolicy_NoOpWhenCentralizedOff(t *testing.T) {
	cache := &TUISecurityPolicyCache{}
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{CentralizedSecurity: false}
	cache.mu.Unlock()
	// Should not panic
	cache.ApplyPolicy()
}

func TestApplyPolicy_RunsWhenCentralizedOn(t *testing.T) {
	cache := &TUISecurityPolicyCache{}
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &TUIHubEffectivePolicy{
			GuardrailMode:   "strict",
			SandboxMode:     "docker",
			NetworkLevel:    "intranet",
			YoloModeAllowed: false,
		},
	}
	cache.mu.Unlock()
	// Should not panic — just logs
	cache.ApplyPolicy()
}

func TestApplyPolicy_CalledAfterUpdate(t *testing.T) {
	// Validates: ApplyPolicy can be called after Update returns true
	cache := &TUISecurityPolicyCache{}
	payload := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {
				"guardrail_mode": "strict",
				"sandbox_mode": "docker",
				"network_level": "intranet",
				"yolo_mode_allowed": false
			}
		}
	}`)

	changed := cache.Update(payload)
	if !changed {
		t.Fatal("expected change on first update")
	}
	// Should not panic
	cache.ApplyPolicy()

	p := cache.Get()
	if p.Policy.GuardrailMode != "strict" {
		t.Errorf("expected strict guardrail mode, got %s", p.Policy.GuardrailMode)
	}
}
