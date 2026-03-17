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
