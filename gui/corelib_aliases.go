package main

// corelib_aliases.go — type aliases that map corelib types into gui's package main.
// This allows gui/ code to continue using bare type names (e.g. AppConfig)
// while the canonical definitions live in corelib/.
// These aliases can be removed incrementally as gui/ code is refactored to
// use qualified corelib.XXX references.

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ── types.go aliases ────────────────────────────────────────────────────────

type ModelConfig = corelib.ModelConfig
type ProjectConfig = corelib.ProjectConfig
type PythonEnvironment = corelib.PythonEnvironment
type ToolConfig = corelib.ToolConfig
type CodeBuddyModel = corelib.CodeBuddyModel
type CodeBuddyFileConfig = corelib.CodeBuddyFileConfig
type MCPServerSource = corelib.MCPServerSource
type MCPServerEntry = corelib.MCPServerEntry
type LocalMCPServerEntry = corelib.LocalMCPServerEntry
type NLSkillStep = corelib.NLSkillStep
type NLSkillEntry = corelib.NLSkillEntry
type MaclawLLMProvider = corelib.MaclawLLMProvider
type MaclawLLMConfig = corelib.MaclawLLMConfig
type SkillHubEntry = corelib.SkillHubEntry
type Skill = corelib.Skill

// ── app_config.go alias ─────────────────────────────────────────────────────

type AppConfig = corelib.AppConfig

// ── corelib/config aliases ──────────────────────────────────────────────────

type ConfigSection = config.ConfigSection
type ConfigKeySchema = config.ConfigKeySchema
type ConfigChange = config.ConfigChange
type ImportReport = config.ImportReport

// ── corelib/memory aliases ──────────────────────────────────────────────────
// NOTE: gui uses MemoryEntry/MemoryCategory (different names from corelib's
// memory.Entry/memory.Category). Only CompressResult shares the same name.

type CompressResult = memory.CompressResult

// ── corelib/tool aliases ────────────────────────────────────────────────────
// NOTE: gui uses different names for most tool types (ToolCategory vs tool.Category,
// ToolRegistry vs tool.Registry, etc.). Only ProgressCallback shares the same name.

type ProgressCallback = tool.ProgressCallback

// ── corelib/security aliases ────────────────────────────────────────────────
// NOTE: gui uses different struct names for implementations (SecurityFirewall
// vs security.Firewall, etc.) so only pure data types are aliased.

type RiskLevel = security.RiskLevel
type RiskAssessment = security.RiskAssessment
type PolicyAction = security.PolicyAction
type PolicyRule = security.PolicyRule
type AuditEntry = security.AuditEntry
type RiskPattern = security.RiskPattern
type AuditAction = security.AuditAction
type AuditFilter = security.AuditFilter

// ── constants ───────────────────────────────────────────────────────────────

const (
	MCPSourceManual  = corelib.MCPSourceManual
	MCPSourceMDNS    = corelib.MCPSourceMDNS
	MCPSourceProject = corelib.MCPSourceProject
)

// Security constants
const (
	RiskLow      = security.RiskLow
	RiskMedium   = security.RiskMedium
	RiskHigh     = security.RiskHigh
	RiskCritical = security.RiskCritical

	PolicyAllow = security.PolicyAllow
	PolicyDeny  = security.PolicyDeny
	PolicyAsk   = security.PolicyAsk
	PolicyAudit = security.PolicyAudit

	AuditActionHubSkillInstall = security.AuditActionHubSkillInstall
	AuditActionHubSkillUpdate  = security.AuditActionHubSkillUpdate
	AuditActionHubSkillReject  = security.AuditActionHubSkillReject
)

// riskLevelOrder re-exports the corelib security level ordering.
var riskLevelOrder = security.RiskLevelOrder

// RequiredNodeVersion re-exports the corelib constant.
const RequiredNodeVersion = corelib.RequiredNodeVersion

// maxAgentIterationsCap re-exports the corelib constant (unexported for gui compatibility).
const maxAgentIterationsCap = config.MaxAgentIterationsCap
