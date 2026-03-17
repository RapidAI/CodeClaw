package main

import (
	"context"
	"fmt"
	"os/exec"
)

// HubSkillUpdateInfo describes an available update for a locally installed Hub Skill.
type HubSkillUpdateInfo struct {
	SkillName      string `json:"skill_name"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HubURL         string `json:"hub_url"`
}

// BackupSkills exports all NL Skills to a zip file (Wails binding).
func (a *App) BackupSkills(outputPath string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.BackupSkills(outputPath)
}

// RestoreSkills imports NL Skills from a zip file (Wails binding).
func (a *App) RestoreSkills(zipPath string) (*RestoreReport, error) {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.RestoreSkills(zipPath)
}

// QueryAuditLog queries the audit log with the given filter (Wails binding).
func (a *App) QueryAuditLog(filter AuditFilter) ([]AuditEntry, error) {
	a.ensureRemoteInfra()
	if a.auditLog == nil {
		return nil, fmt.Errorf("audit log not initialized")
	}
	return a.auditLog.Query(filter)
}

// RecommendTool suggests the best programming tool for a task (Wails binding).
func (a *App) RecommendTool(taskDescription string) (string, string) {
	a.ensureRemoteInfra()
	if a.toolSelector == nil {
		return "", "tool selector not initialized"
	}
	// Get installed tools by checking which known tools have their binary available.
	var installed []string
	for _, tool := range []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	return a.toolSelector.Recommend(taskDescription, installed)
}

// SearchSkillHub searches configured SkillHubs for Skills matching the query (Wails binding).
func (a *App) SearchSkillHub(query string) ([]HubSkillMeta, error) {
	a.ensureRemoteInfra()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	return a.skillHubClient.Search(context.Background(), query)
}

// InstallHubSkill downloads a Skill from the specified Hub and registers it locally (Wails binding).
func (a *App) InstallHubSkill(skillID, hubURL string) error {
	a.ensureRemoteInfra()
	if a.skillHubClient == nil {
		return fmt.Errorf("skill hub client not initialized")
	}
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	entry, err := a.skillHubClient.Install(context.Background(), skillID, hubURL)
	if err != nil {
		return err
	}
	return a.skillExecutor.Register(*entry)
}

// CheckHubSkillUpdates checks all locally installed Hub Skills for available updates (Wails binding).
func (a *App) CheckHubSkillUpdates() ([]HubSkillUpdateInfo, error) {
	a.ensureRemoteInfra()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}

	skills := a.skillExecutor.loadSkills()
	var updates []HubSkillUpdateInfo
	ctx := context.Background()

	for _, s := range skills {
		if s.Source != "hub" || s.HubSkillID == "" {
			continue
		}
		meta, err := a.skillHubClient.CheckUpdate(ctx, s.HubSkillID, s.HubVersion)
		if err != nil || meta == nil {
			continue
		}
		updates = append(updates, HubSkillUpdateInfo{
			SkillName:      s.Name,
			CurrentVersion: s.HubVersion,
			LatestVersion:  meta.Version,
			HubURL:         meta.HubURL,
		})
	}
	return updates, nil
}

// UpdateHubSkill updates a locally installed Hub Skill to the latest version (Wails binding).
func (a *App) UpdateHubSkill(skillName string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.UpdateFromHub(skillName)
}

// ---------------------------------------------------------------------------
// Memory management Wails bindings
// ---------------------------------------------------------------------------

// ListMemories returns memory entries filtered by category and keyword (Wails binding).
func (a *App) ListMemories(category, keyword string) []MemoryEntry {
	a.ensureRemoteInfra()
	if a.memoryStore == nil {
		return nil
	}
	return a.memoryStore.List(MemoryCategory(category), keyword)
}

// DeleteMemory removes the memory entry with the given ID (Wails binding).
func (a *App) DeleteMemory(id string) error {
	a.ensureRemoteInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return a.memoryStore.Delete(id)
}

// ---------------------------------------------------------------------------
// Session template Wails bindings
// ---------------------------------------------------------------------------

// ListTemplates returns all session templates (Wails binding).
func (a *App) ListTemplates() []SessionTemplate {
	a.ensureRemoteInfra()
	if a.templateManager == nil {
		return nil
	}
	return a.templateManager.List()
}

// CreateTemplate creates a new session template (Wails binding).
func (a *App) CreateTemplate(name, tool, projectPath, modelConfig string, yoloMode bool) error {
	a.ensureRemoteInfra()
	if a.templateManager == nil {
		return fmt.Errorf("template manager not initialized")
	}
	return a.templateManager.Create(SessionTemplate{
		Name:        name,
		Tool:        tool,
		ProjectPath: projectPath,
		ModelConfig: modelConfig,
		YoloMode:    yoloMode,
	})
}

// DeleteTemplate removes the session template with the given name (Wails binding).
func (a *App) DeleteTemplate(name string) error {
	a.ensureRemoteInfra()
	if a.templateManager == nil {
		return fmt.Errorf("template manager not initialized")
	}
	return a.templateManager.Delete(name)
}

// ---------------------------------------------------------------------------
// Configuration management Wails bindings
// ---------------------------------------------------------------------------

// GetConfigSchema returns the configuration schema as JSON (Wails binding).
func (a *App) GetConfigSchema() (string, error) {
	a.ensureRemoteInfra()
	if a.configManager == nil {
		return "", fmt.Errorf("config manager not initialized")
	}
	return a.configManager.SchemaJSON()
}

// UpdateConfigBinding modifies a single configuration key and returns the old value (Wails binding).
func (a *App) UpdateConfigBinding(section, key, value string) (string, error) {
	a.ensureRemoteInfra()
	if a.configManager == nil {
		return "", fmt.Errorf("config manager not initialized")
	}
	return a.configManager.UpdateConfig(section, key, value)
}
