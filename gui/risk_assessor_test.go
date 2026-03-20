package main

import (
	"testing"
)

func TestAssessSkill_EmptySteps(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name:  "empty-skill",
		Steps: []NLSkillStep{},
	}
	result := ra.AssessSkill(skill, "community")
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow for empty steps, got %s", result.Level)
	}
}

func TestAssessSkill_ReadOnlySteps(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "read-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	result := ra.AssessSkill(skill, "community")
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow for read-only steps, got %s", result.Level)
	}
}

func TestAssessSkill_WriteStep_Medium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	result := ra.AssessSkill(skill, "community")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium for write step, got %s", result.Level)
	}
}

func TestAssessSkill_DangerousKeyword_Critical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "dangerous-skill",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "rm -rf /"}},
		},
	}
	result := ra.AssessSkill(skill, "community")
	if result.Level != RiskCritical {
		t.Errorf("expected RiskCritical for dangerous keyword, got %s", result.Level)
	}
}

func TestAssessSkill_OfficialTrust_DowngradesMediumToLow(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "official-write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	result := ra.AssessSkill(skill, "official")
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow (official downgrade from medium), got %s", result.Level)
	}
	found := false
	for _, f := range result.Factors {
		if f == "official trust level: medium downgraded to low" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected downgrade factor in factors list")
	}
}

func TestAssessSkill_UnknownTrust_UpgradesLowToMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "unknown-read-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	result := ra.AssessSkill(skill, "unknown")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (unknown upgrade from low), got %s", result.Level)
	}
	found := false
	for _, f := range result.Factors {
		if f == "unknown trust level: low upgraded to medium" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected upgrade factor in factors list")
	}
}

func TestAssessSkill_TakesHighestRisk(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "mixed-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	result := ra.AssessSkill(skill, "community")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (highest of read=low, write=medium), got %s", result.Level)
	}
}

func TestAssessSkill_OfficialDoesNotDowngradeCritical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "critical-official",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	result := ra.AssessSkill(skill, "official")
	if result.Level != RiskCritical {
		t.Errorf("expected RiskCritical (official should not downgrade critical), got %s", result.Level)
	}
}

func TestAssessSkill_UnknownDoesNotUpgradeMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "unknown-write",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	result := ra.AssessSkill(skill, "unknown")
	// Write is medium, unknown only upgrades low→medium, so medium stays medium
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (unknown does not upgrade medium), got %s", result.Level)
	}
}
