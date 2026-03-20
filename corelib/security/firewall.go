package security

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Firewall integrates RiskAnalyzer + PolicyEngine + AuditLog to provide
// a unified security check before tool execution.
type Firewall struct {
	analyzer *RiskAnalyzer
	policy   *PolicyEngine
	audit    *AuditLog
	onAsk    func(toolName string, risk RiskAssessment) (bool, error)

	sessionApprovals map[string]map[string]bool
	mu               sync.RWMutex
}

// NewFirewall creates a firewall combining the three security components.
func NewFirewall(analyzer *RiskAnalyzer, policy *PolicyEngine, audit *AuditLog) *Firewall {
	return &Firewall{
		analyzer:         analyzer,
		policy:           policy,
		audit:            audit,
		sessionApprovals: make(map[string]map[string]bool),
	}
}

// SetOnAsk sets the callback for user confirmation when policy action is "ask".
func (f *Firewall) SetOnAsk(fn func(toolName string, risk RiskAssessment) (bool, error)) {
	f.onAsk = fn
}

// Check performs a security check before tool execution.
func (f *Firewall) Check(toolName string, args map[string]interface{}, ctx *CallContext) (bool, string) {
	if f.analyzer == nil {
		return true, ""
	}

	risk := f.analyzer.Assess(toolName, args, ctx)

	sessionID := ""
	if ctx != nil {
		sessionID = ctx.SessionID
	}
	if sessionID != "" && f.isSessionApproved(sessionID, toolName) {
		f.recordAudit(toolName, args, risk, PolicyAudit, "session_approved", sessionID)
		return true, ""
	}

	action := PolicyAllow
	if f.policy != nil {
		action = f.policy.Evaluate(toolName, args, risk.Level)
	}

	f.recordAudit(toolName, args, risk, action, "", sessionID)

	switch action {
	case PolicyAllow, PolicyAudit:
		return true, ""
	case PolicyDeny:
		return false, fmt.Sprintf("⛔ 安全策略拒绝: %s (风险等级: %s, 原因: %s)", toolName, risk.Level, risk.Reason)
	case PolicyAsk:
		if f.onAsk != nil {
			approved, err := f.onAsk(toolName, risk)
			if err != nil {
				return false, fmt.Sprintf("⛔ 用户确认失败: %v", err)
			}
			if approved {
				if sessionID != "" {
					f.approveForSession(sessionID, toolName)
				}
				return true, ""
			}
			return false, fmt.Sprintf("⛔ 用户拒绝执行: %s", toolName)
		}
		if risk.Level == RiskHigh || risk.Level == RiskCritical {
			return false, fmt.Sprintf("⚠️ 高风险操作需要确认但无确认通道: %s (风险: %s, 原因: %s)", toolName, risk.Level, risk.Reason)
		}
		return true, ""
	default:
		return true, ""
	}
}

func (f *Firewall) recordAudit(toolName string, args map[string]interface{}, risk RiskAssessment, action PolicyAction, result, sessionID string) {
	if f.audit == nil {
		return
	}
	if result == "" {
		result = string(action)
	}
	_ = f.audit.Log(AuditEntry{
		Timestamp:    time.Now(),
		SessionID:    sessionID,
		ToolName:     toolName,
		Arguments:    args,
		RiskLevel:    risk.Level,
		PolicyAction: action,
		Result:       result,
	})
}

func (f *Firewall) isSessionApproved(sessionID, toolName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	approvals, ok := f.sessionApprovals[sessionID]
	if !ok {
		return false
	}
	if approvals[toolName] || approvals["*"] {
		return true
	}
	for pattern := range approvals {
		if pattern != "" && pattern != toolName && strings.Contains(toolName, pattern) {
			return true
		}
	}
	return false
}

func (f *Firewall) approveForSession(sessionID, toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionApprovals[sessionID] == nil {
		f.sessionApprovals[sessionID] = make(map[string]bool)
	}
	f.sessionApprovals[sessionID][toolName] = true
}

// ApproveForSession explicitly approves a tool pattern for a session.
func (f *Firewall) ApproveForSession(sessionID, toolPattern string) {
	f.approveForSession(sessionID, toolPattern)
}

// ClearSession removes all session-level approvals for a session.
func (f *Firewall) ClearSession(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessionApprovals, sessionID)
}

// LoadProjectPolicy loads project-level security policy from a file.
func (f *Firewall) LoadProjectPolicy(projectPath string) error {
	if f.policy == nil {
		return nil
	}
	policyPath := projectPath + "/.maclaw/security-policy.json"
	return f.policy.LoadRules(policyPath)
}
