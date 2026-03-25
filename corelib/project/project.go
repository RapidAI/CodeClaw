// Package project provides shared project management operations
// used by both TUI and GUI agents.
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ConfigStore abstracts config load/save so both TUI (FileConfigStore)
// and GUI (App) can plug in.
type ConfigStore interface {
	LoadConfig() (corelib.AppConfig, error)
	SaveConfig(corelib.AppConfig) error
}

// --- Result types ---

type CreateResult struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type ListItem struct {
	Id      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Current bool   `json:"current"`
}

type DeleteResult struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type SwitchResult struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// --- Core operations ---

func Create(store ConfigStore, name, rawPath string) (*CreateResult, error) {
	if name == "" || rawPath == "" {
		return nil, fmt.Errorf("name and path are required")
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	absPath = filepath.Clean(absPath)
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	for _, p := range cfg.Projects {
		if strings.EqualFold(p.Name, name) {
			return nil, fmt.Errorf("project %q already exists", name)
		}
		if filepath.Clean(p.Path) == absPath {
			return nil, fmt.Errorf("path %q already registered as project %q", absPath, p.Name)
		}
	}
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return nil, fmt.Errorf("create project directory: %w", err)
	}
	id := fmt.Sprintf("proj_%s_%d", SanitizeName(name), time.Now().UnixMilli())
	cfg.Projects = append(cfg.Projects, corelib.ProjectConfig{Id: id, Name: name, Path: absPath})
	if err := store.SaveConfig(cfg); err != nil {
		return nil, err
	}
	return &CreateResult{Id: id, Name: name, Path: absPath}, nil
}

func List(store ConfigStore) ([]ListItem, error) {
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		items = append(items, ListItem{
			Id: p.Id, Name: p.Name, Path: p.Path,
			Current: p.Id == cfg.CurrentProject,
		})
	}
	return items, nil
}

func Delete(store ConfigStore, target string) (*DeleteResult, error) {
	if target == "" {
		return nil, fmt.Errorf("target is required")
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	idx, _ := findProject(&cfg, target)
	if idx < 0 {
		return nil, fmt.Errorf("project %q not found", target)
	}
	removed := cfg.Projects[idx]
	cfg.Projects = append(cfg.Projects[:idx], cfg.Projects[idx+1:]...)
	if cfg.CurrentProject == removed.Id {
		cfg.CurrentProject = ""
	}
	if err := store.SaveConfig(cfg); err != nil {
		return nil, err
	}
	return &DeleteResult{Id: removed.Id, Name: removed.Name}, nil
}

func Switch(store ConfigStore, target string) (*SwitchResult, error) {
	if target == "" {
		return nil, fmt.Errorf("target is required")
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	_, found := findProject(&cfg, target)
	if found == nil {
		return nil, fmt.Errorf("project %q not found", target)
	}
	cfg.CurrentProject = found.Id
	if err := store.SaveConfig(cfg); err != nil {
		return nil, err
	}
	return &SwitchResult{Id: found.Id, Name: found.Name, Path: found.Path}, nil
}

// --- Helpers ---

func findProject(cfg *corelib.AppConfig, target string) (int, *corelib.ProjectConfig) {
	for i, p := range cfg.Projects {
		if strings.EqualFold(p.Name, target) || strings.EqualFold(p.Id, target) {
			return i, &cfg.Projects[i]
		}
	}
	return -1, nil
}

// SanitizeName converts a project name to a safe ID fragment.
// Non-ASCII characters (e.g. Chinese) are replaced with "-" and consecutive
// dashes are collapsed.
func SanitizeName(name string) string {
	var b strings.Builder
	prevDash := true // suppress leading dash
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_':
			if !prevDash {
				b.WriteRune(r)
				prevDash = true
			}
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-_")
	if s == "" {
		s = "project"
	}
	if len(s) > 20 {
		s = strings.TrimRight(s[:20], "-_")
	}
	if s == "" {
		s = "project"
	}
	return s
}
