package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RunSkill 执行 skill 子命令。
func RunSkill(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui skill <list>")
	}
	switch args[0] {
	case "list":
		return skillList(args[1:])
	default:
		return NewUsageError("unknown skill action: %s", args[0])
	}
}

// localSkillInfo 本地技能信息（从 YAML 读取）。
type localSkillInfo struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Triggers    []string `yaml:"triggers" json:"triggers"`
	Status      string   `yaml:"status" json:"status"`
}

func skillList(args []string) error {
	fs := flag.NewFlagSet("skill list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	skillsRoot := filepath.Join(home, ".maclaw", "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			if *jsonOut {
				return PrintJSON([]localSkillInfo{})
			}
			fmt.Println("No skills found. Skills directory does not exist.")
			fmt.Printf("  Path: %s\n", skillsRoot)
			return nil
		}
		return err
	}

	var skills []localSkillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		yamlPath := filepath.Join(skillsRoot, entry.Name(), "skill.yaml")
		data, readErr := os.ReadFile(yamlPath)
		if readErr != nil {
			yamlPath = filepath.Join(skillsRoot, entry.Name(), "skill.yml")
			data, readErr = os.ReadFile(yamlPath)
			if readErr != nil {
				continue
			}
		}
		var info localSkillInfo
		if err := yaml.Unmarshal(data, &info); err != nil {
			continue
		}
		if info.Name == "" {
			info.Name = entry.Name()
		}
		if info.Status == "" {
			info.Status = "active"
		}
		skills = append(skills, info)
	}

	if *jsonOut {
		return PrintJSON(skills)
	}
	if len(skills) == 0 {
		fmt.Println("No skills found.")
		fmt.Printf("  Skills directory: %s\n", skillsRoot)
		return nil
	}
	fmt.Printf("%-20s %-8s %-30s %s\n", "NAME", "STATUS", "TRIGGERS", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 85))
	for _, s := range skills {
		triggers := strings.Join(s.Triggers, ", ")
		fmt.Printf("%-20s %-8s %-30s %s\n",
			TruncateDisplay(s.Name, 20),
			s.Status,
			TruncateDisplay(triggers, 30),
			TruncateDisplay(s.Description, 40))
	}
	return nil
}
