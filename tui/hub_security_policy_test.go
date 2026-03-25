package main

import (
	"encoding/json"
	"testing"
)

func TestTUISecurityCacheUpdate_ParsesPolicy(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	payload := json.RawMessage(`{
		"request_id": "abc",
		"security_policy": {
			"centralized_security": true,
			"policy": {
				"file_outbound_enabled": false,
				"image_outbound_enabled": true,
				"gossip_enabled": false,
				"guardrail_mode": "strict",
				"sandbox_mode": "docker",
				"network_level": "intranet",
				"yolo_mode_allowed": false,
				"smart_route_enabled": true
			}
		}
	}`)

	changed := cache.Update(payload)
	if !changed {
		t.Fatal("expected policy change on first update")
	}

	got := cache.Get()
	if got == nil {
		t.Fatal("expected non-nil policy")
	}
	if !got.CentralizedSecurity {
		t.Error("expected centralized_security=true")
	}
	if got.Policy == nil {
		t.Fatal("expected non-nil effective policy")
	}
	if got.Policy.FileOutboundEnabled {
		t.Error("expected file_outbound_enabled=false")
	}
	if got.Policy.GuardrailMode != "strict" {
		t.Errorf("expected guardrail_mode=strict, got %s", got.Policy.GuardrailMode)
	}
	if got.Policy.YoloModeAllowed {
		t.Error("expected yolo_mode_allowed=false")
	}
}

func TestTUISecurityCacheUpdate_NoChangeOnSamePolicy(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	payload := json.RawMessage(`{
		"security_policy": {
			"centralized_security": false
		}
	}`)

	changed := cache.Update(payload)
	if !changed {
		t.Fatal("expected change on first update")
	}

	changed = cache.Update(payload)
	if changed {
		t.Error("expected no change on identical update")
	}
}

func TestTUISecurityCacheUpdate_NoSecurityPolicyField(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	payload := json.RawMessage(`{"request_id": "xyz"}`)
	changed := cache.Update(payload)
	if changed {
		t.Error("expected no change when security_policy is absent")
	}
	if cache.Get() != nil {
		t.Error("expected nil policy when never set")
	}
}

func TestTUISecurityCacheUpdate_InvalidJSON(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	payload := json.RawMessage(`not valid json`)
	changed := cache.Update(payload)
	if changed {
		t.Error("expected no change on invalid JSON")
	}
}

func TestTUISecurityCacheUpdate_DetectsChange(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	p1 := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {"guardrail_mode": "standard"}
		}
	}`)
	p2 := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {"guardrail_mode": "strict"}
		}
	}`)

	cache.Update(p1)
	changed := cache.Update(p2)
	if !changed {
		t.Error("expected change when policy differs")
	}
	if cache.Get().Policy.GuardrailMode != "strict" {
		t.Error("expected updated guardrail_mode")
	}
}

func TestTUISecurityCache_PreservedOnDisconnect(t *testing.T) {
	// Validates: Requirement 7.2 — cache persists after disconnect
	cache := &TUISecurityPolicyCache{}

	payload := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {"gossip_enabled": false}
		}
	}`)
	cache.Update(payload)

	// After disconnect, Get() should still return the cached policy.
	got := cache.Get()
	if got == nil {
		t.Fatal("expected cached policy to persist after disconnect")
	}
	if got.Policy.GossipEnabled {
		t.Error("expected gossip_enabled=false from cached policy")
	}
}

// ── Helper method tests ─────────────────────────────────────────────────────

func TestTUISecurityCache_IsGossipAllowed(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	// No cached policy → allowed
	if !cache.IsGossipAllowed() {
		t.Error("expected gossip allowed when no cached policy")
	}

	// Centralized off → allowed
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &TUIHubEffectivePolicy{GossipEnabled: false},
	}
	cache.mu.Unlock()
	if !cache.IsGossipAllowed() {
		t.Error("expected gossip allowed when centralized security is off")
	}

	// Centralized on, gossip disabled → forbidden
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{GossipEnabled: false},
	}
	cache.mu.Unlock()
	if cache.IsGossipAllowed() {
		t.Error("expected gossip forbidden when policy disallows it")
	}

	// Centralized on, gossip enabled → allowed
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{GossipEnabled: true},
	}
	cache.mu.Unlock()
	if !cache.IsGossipAllowed() {
		t.Error("expected gossip allowed when policy permits it")
	}

	// Centralized on, nil effective policy → allowed (fail-open)
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              nil,
	}
	cache.mu.Unlock()
	if !cache.IsGossipAllowed() {
		t.Error("expected gossip allowed when effective policy is nil")
	}
}

func TestTUISecurityCache_IsYoloModeAllowed(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	// No cached policy → allowed
	if !cache.IsYoloModeAllowed() {
		t.Error("expected YOLO allowed when no cached policy")
	}

	// Centralized off → allowed
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &TUIHubEffectivePolicy{YoloModeAllowed: false},
	}
	cache.mu.Unlock()
	if !cache.IsYoloModeAllowed() {
		t.Error("expected YOLO allowed when centralized security is off")
	}

	// Centralized on, yolo forbidden → forbidden
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{YoloModeAllowed: false},
	}
	cache.mu.Unlock()
	if cache.IsYoloModeAllowed() {
		t.Error("expected YOLO forbidden when policy disallows it")
	}

	// Centralized on, yolo allowed → allowed
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{YoloModeAllowed: true},
	}
	cache.mu.Unlock()
	if !cache.IsYoloModeAllowed() {
		t.Error("expected YOLO allowed when policy permits it")
	}
}

func TestTUISecurityCache_GetGuardrailMode(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	if got := cache.GetGuardrailMode(); got != "" {
		t.Errorf("expected empty guardrail mode, got %q", got)
	}

	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{GuardrailMode: "strict"},
	}
	cache.mu.Unlock()
	if got := cache.GetGuardrailMode(); got != "strict" {
		t.Errorf("expected strict, got %q", got)
	}
}

func TestTUISecurityCache_GetSandboxMode(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	if got := cache.GetSandboxMode(); got != "" {
		t.Errorf("expected empty sandbox mode, got %q", got)
	}

	// Centralized off → empty
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &TUIHubEffectivePolicy{SandboxMode: "docker"},
	}
	cache.mu.Unlock()
	if got := cache.GetSandboxMode(); got != "" {
		t.Errorf("expected empty when centralized off, got %q", got)
	}

	// Centralized on → returns value
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{SandboxMode: "docker"},
	}
	cache.mu.Unlock()
	if got := cache.GetSandboxMode(); got != "docker" {
		t.Errorf("expected docker, got %q", got)
	}
}

func TestTUISecurityCache_GetNetworkLevel(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	if got := cache.GetNetworkLevel(); got != "" {
		t.Errorf("expected empty network level, got %q", got)
	}

	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &TUIHubEffectivePolicy{NetworkLevel: "intranet"},
	}
	cache.mu.Unlock()
	if got := cache.GetNetworkLevel(); got != "intranet" {
		t.Errorf("expected intranet, got %q", got)
	}
}

func TestTUISecurityCache_IsReadOnly(t *testing.T) {
	cache := &TUISecurityPolicyCache{}

	// No cached policy → not read-only
	if cache.IsReadOnly() {
		t.Error("expected not read-only when no cached policy")
	}

	// Centralized off → not read-only
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{CentralizedSecurity: false}
	cache.mu.Unlock()
	if cache.IsReadOnly() {
		t.Error("expected not read-only when centralized is off")
	}

	// Centralized on → read-only
	cache.mu.Lock()
	cache.policy = &TUIHubSecurityPolicy{CentralizedSecurity: true}
	cache.mu.Unlock()
	if !cache.IsReadOnly() {
		t.Error("expected read-only when centralized is on")
	}
}
