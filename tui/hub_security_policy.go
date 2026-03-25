package main

import (
	"encoding/json"
	"log"
	"reflect"
	"sync"
)

// TUIHubEffectivePolicy mirrors hub/internal/security.EffectivePolicy on the TUI client side.
// Defined locally to avoid importing the hub internal package.
type TUIHubEffectivePolicy struct {
	FileOutboundEnabled  bool   `json:"file_outbound_enabled"`
	ImageOutboundEnabled bool   `json:"image_outbound_enabled"`
	GossipEnabled        bool   `json:"gossip_enabled"`
	GuardrailMode        string `json:"guardrail_mode"`
	SandboxMode          string `json:"sandbox_mode"`
	NetworkLevel         string `json:"network_level"`
	YoloModeAllowed      bool   `json:"yolo_mode_allowed"`
	SmartRouteEnabled    bool   `json:"smart_route_enabled"`
}

// TUIHubSecurityPolicy mirrors hub/internal/security.HeartbeatSecurityPayload on the TUI client side.
type TUIHubSecurityPolicy struct {
	CentralizedSecurity bool                   `json:"centralized_security"`
	Policy              *TUIHubEffectivePolicy `json:"policy,omitempty"`
}

// TUISecurityPolicyCache holds the cached security policy received from Hub heartbeat acks.
// Thread-safe via mu. On disconnect the cache is intentionally NOT cleared so the
// last-known policy continues to apply (requirement 7.2).
type TUISecurityPolicyCache struct {
	mu     sync.RWMutex
	policy *TUIHubSecurityPolicy
}

// Get returns the currently cached policy (may be nil if never received).
func (c *TUISecurityPolicyCache) Get() *TUIHubSecurityPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.policy
}

// Update parses the security_policy field from a heartbeat ack payload,
// updates the cache, and returns true if the policy changed.
func (c *TUISecurityPolicyCache) Update(raw json.RawMessage) bool {
	var wrapper struct {
		SecurityPolicy *TUIHubSecurityPolicy `json:"security_policy"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		log.Printf("[tui-hub-security] failed to parse ack payload: %v", err)
		return false
	}
	if wrapper.SecurityPolicy == nil {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	changed := !reflect.DeepEqual(c.policy, wrapper.SecurityPolicy)
	if changed {
		c.policy = wrapper.SecurityPolicy
	}
	return changed
}

// IsGossipAllowed checks whether Gossip functionality is permitted given the
// current Hub security policy. When centralized security is enabled and the
// effective policy sets gossip_enabled=false, this returns false (Req 6.2).
func (c *TUISecurityPolicyCache) IsGossipAllowed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.policy == nil || !c.policy.CentralizedSecurity || c.policy.Policy == nil {
		return true // no centralized policy — allow local preference
	}
	return c.policy.Policy.GossipEnabled
}

// IsYoloModeAllowed checks whether YOLO mode is permitted given the current
// Hub security policy. When centralized security is enabled and the effective
// policy sets yolo_mode_allowed=false, this returns false (Req 7.8).
func (c *TUISecurityPolicyCache) IsYoloModeAllowed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.policy == nil || !c.policy.CentralizedSecurity || c.policy.Policy == nil {
		return true
	}
	return c.policy.Policy.YoloModeAllowed
}

// GetGuardrailMode returns the Hub-enforced guardrail mode, or empty string
// if centralized security is not active.
func (c *TUISecurityPolicyCache) GetGuardrailMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.policy == nil || !c.policy.CentralizedSecurity || c.policy.Policy == nil {
		return ""
	}
	return c.policy.Policy.GuardrailMode
}

// GetSandboxMode returns the Hub-enforced sandbox mode, or empty string
// if centralized security is not active.
func (c *TUISecurityPolicyCache) GetSandboxMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.policy == nil || !c.policy.CentralizedSecurity || c.policy.Policy == nil {
		return ""
	}
	return c.policy.Policy.SandboxMode
}

// GetNetworkLevel returns the Hub-enforced network access level, or empty
// string if centralized security is not active.
func (c *TUISecurityPolicyCache) GetNetworkLevel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.policy == nil || !c.policy.CentralizedSecurity || c.policy.Policy == nil {
		return ""
	}
	return c.policy.Policy.NetworkLevel
}

// IsReadOnly returns true when centralized security is enabled,
// meaning the local security settings should be read-only.
func (c *TUISecurityPolicyCache) IsReadOnly() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.policy != nil && c.policy.CentralizedSecurity
}

// tuiSecurityPolicy is the package-level security policy cache accessible from TUI commands.
var tuiSecurityPolicy TUISecurityPolicyCache

// ApplyPolicy enforces the Hub-pushed security policy on local TUI components.
// Called after Update() returns true to log and apply enforcement actions.
//
// Enforcement actions (Requirements 7.5, 7.6, 7.7, 7.8):
//   - guardrail_mode  → logged for observability, consulted at tool-execution time
//   - sandbox_mode    → logged for observability, consulted at tool-execution time
//   - network_level   → logged for observability, consulted at tool-execution time
//   - yolo_mode_allowed=false → YOLO mode is force-disabled at launch time
func (c *TUISecurityPolicyCache) ApplyPolicy() {
	p := c.Get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return
	}
	ep := p.Policy
	if ep.GuardrailMode != "" {
		log.Printf("[tui-hub-security] guardrail_mode applied: %s", ep.GuardrailMode)
	}
	if ep.SandboxMode != "" {
		log.Printf("[tui-hub-security] sandbox_mode applied: %s", ep.SandboxMode)
	}
	if ep.NetworkLevel != "" {
		log.Printf("[tui-hub-security] network_level applied: %s", ep.NetworkLevel)
	}
	if !ep.YoloModeAllowed {
		log.Printf("[tui-hub-security] yolo_mode forced disabled by hub policy")
	}
}
