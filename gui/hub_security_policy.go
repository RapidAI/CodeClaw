package main

import (
	"encoding/json"
	"log"
	"reflect"
	"sync"
)

// HubEffectivePolicy mirrors hub/internal/security.EffectivePolicy on the client side.
// We define a local copy to avoid importing the hub internal package.
type HubEffectivePolicy struct {
	FileOutboundEnabled  bool   `json:"file_outbound_enabled"`
	ImageOutboundEnabled bool   `json:"image_outbound_enabled"`
	GossipEnabled        bool   `json:"gossip_enabled"`
	GuardrailMode        string `json:"guardrail_mode"`
	SandboxMode          string `json:"sandbox_mode"`
	NetworkLevel         string `json:"network_level"`
	YoloModeAllowed      bool   `json:"yolo_mode_allowed"`
	SmartRouteEnabled    bool   `json:"smart_route_enabled"`
}

// HubSecurityPolicy mirrors hub/internal/security.HeartbeatSecurityPayload on the client side.
type HubSecurityPolicy struct {
	CentralizedSecurity bool                `json:"centralized_security"`
	Policy              *HubEffectivePolicy `json:"policy,omitempty"`
}

// hubSecurityCache holds the cached security policy received from Hub heartbeat acks.
// Thread-safe via mu. On disconnect the cache is intentionally NOT cleared so the
// last-known policy continues to apply (requirement 7.2).
type hubSecurityCache struct {
	mu     sync.RWMutex
	policy *HubSecurityPolicy
}

// get returns the currently cached policy (may be nil if never received).
func (c *hubSecurityCache) get() *HubSecurityPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.policy
}

// update parses the security_policy field from a heartbeat ack payload,
// updates the cache, and returns true if the policy changed.
func (c *hubSecurityCache) update(raw json.RawMessage) bool {
	var wrapper struct {
		SecurityPolicy *HubSecurityPolicy `json:"security_policy"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		log.Printf("[hub-security] failed to parse ack payload: %v", err)
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

// GetHubSecurityPolicy returns the current Hub security policy (exposed to frontend via Wails).
func (a *App) GetHubSecurityPolicy() *HubSecurityPolicy {
	return a.hubSecurityCache.get()
}

// IsHubSecurityReadOnly returns true when centralized security is enabled,
// meaning the local security settings UI should be read-only.
func (a *App) IsHubSecurityReadOnly() bool {
	p := a.hubSecurityCache.get()
	return p != nil && p.CentralizedSecurity
}

// updateHubSecurityPolicy is called from the readLoop when an ack message arrives.
// It parses the payload, updates the cache, and emits an event if the policy changed.
// When the policy changes, it also applies the enforcement actions (Req 7.5-7.8, 4.5).
func (a *App) updateHubSecurityPolicy(payload json.RawMessage) {
	if a.hubSecurityCache.update(payload) {
		policy := a.hubSecurityCache.get()
		a.applyHubSecurityPolicy(policy)
		a.emitEvent("hub-security-policy-changed", policy)
		log.Printf("[hub-security] policy updated: centralized=%v", policy.CentralizedSecurity)
	}
}

// applyHubSecurityPolicy enforces the Hub-pushed security policy on local components.
//
// Enforcement actions (Requirements 4.5, 7.5, 7.6, 7.7, 7.8):
//   - guardrail_mode  → PolicyEngine.SetMode
//   - sandbox_mode    → stored for Firewall sandbox configuration
//   - network_level   → stored for network access level enforcement
//   - yolo_mode_allowed=false → YOLO mode is force-disabled at launch time
//   - centralized_security=true → frontend switches to read-only mode (via event)
func (a *App) applyHubSecurityPolicy(policy *HubSecurityPolicy) {
	if policy == nil {
		return
	}

	if !policy.CentralizedSecurity || policy.Policy == nil {
		// Centralized security is off — no enforcement needed.
		// The frontend event will restore editable mode.
		return
	}

	ep := policy.Policy

	// 1. guardrail_mode → PolicyEngine.SetMode (Req 7.5)
	if ep.GuardrailMode != "" && a.policyEngine != nil {
		a.policyEngine.SetMode(ep.GuardrailMode)
		log.Printf("[hub-security] guardrail_mode applied: %s", ep.GuardrailMode)
	}

	// 2. sandbox_mode → update Firewall sandbox configuration (Req 7.6)
	// The sandbox mode is stored in the cached policy and consulted at tool-execution
	// time by the SecurityFirewall. Log the change for observability.
	if ep.SandboxMode != "" {
		log.Printf("[hub-security] sandbox_mode applied: %s", ep.SandboxMode)
	}

	// 3. network_level → update network access level (Req 7.7)
	// The network level is stored in the cached policy and consulted at tool-execution
	// time. Log the change for observability.
	if ep.NetworkLevel != "" {
		log.Printf("[hub-security] network_level applied: %s", ep.NetworkLevel)
	}

	// 4. yolo_mode_allowed=false → force-disable YOLO mode (Req 7.8)
	// The actual override happens in isYoloModeAllowed() which is checked at launch time.
	// Log when YOLO is being restricted.
	if !ep.YoloModeAllowed {
		log.Printf("[hub-security] yolo_mode forced disabled by hub policy")
	}
}

// isGossipAllowed checks whether Gossip functionality is permitted given the
// current Hub security policy. When centralized security is enabled and the
// effective policy sets gossip_enabled=false, this returns false (Req 6.1, 6.3, 6.4).
func (a *App) isGossipAllowed() bool {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return true // no centralized policy — allow local preference
	}
	return p.Policy.GossipEnabled
}

// IsGossipAllowed returns whether Gossip is allowed (exposed to frontend via Wails).
// The frontend uses this to hide/show the Gossip sidebar icon and panel.
func (a *App) IsGossipAllowed() bool {
	return a.isGossipAllowed()
}

// isYoloModeAllowed checks whether YOLO mode is permitted given the current
// Hub security policy. When centralized security is enabled and the effective
// policy sets yolo_mode_allowed=false, this returns false regardless of the
// user's local preference (Req 7.8).
func (a *App) isYoloModeAllowed() bool {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return true // no centralized policy — allow local preference
	}
	return p.Policy.YoloModeAllowed
}

// getHubSandboxMode returns the Hub-enforced sandbox mode, or empty string
// if centralized security is not active.
func (a *App) getHubSandboxMode() string {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return ""
	}
	return p.Policy.SandboxMode
}

// getHubNetworkLevel returns the Hub-enforced network access level, or empty
// string if centralized security is not active.
func (a *App) getHubNetworkLevel() string {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return ""
	}
	return p.Policy.NetworkLevel
}

// enforceYoloMode applies the Hub YOLO override to a launch spec's YoloMode flag.
// Returns the (possibly overridden) value and a human-readable reason if overridden.
func (a *App) enforceYoloMode(requested bool) (bool, string) {
	if requested && !a.isYoloModeAllowed() {
		return false, "YOLO 模式已被 Hub 安全策略禁止"
	}
	return requested, ""
}

// enforceYoloModeQuiet is a convenience wrapper that returns only the bool.
// Used in launch spec construction where the reason string is not needed.
func (a *App) enforceYoloModeQuiet(requested bool) bool {
	v, _ := a.enforceYoloMode(requested)
	return v
}
