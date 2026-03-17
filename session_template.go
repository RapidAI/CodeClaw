package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionTemplate represents a reusable session launch configuration.
type SessionTemplate struct {
	Name        string            `json:"name"`
	Tool        string            `json:"tool"`
	ProjectPath string            `json:"project_path"`
	ModelConfig string            `json:"model_config"`
	YoloMode    bool              `json:"yolo_mode"`
	EnvVars     map[string]string `json:"env_vars,omitempty"`
	CreatedAt   string            `json:"created_at"`
}

// SessionTemplateManager manages CRUD operations for session templates
// with JSON file persistence.
type SessionTemplateManager struct {
	mu        sync.RWMutex
	templates []SessionTemplate
	path      string // persistence path, e.g. ~/.maclaw/templates.json
}

// NewSessionTemplateManager creates a SessionTemplateManager that persists
// to the given path. It loads existing templates from disk if the file exists.
func NewSessionTemplateManager(path string) (*SessionTemplateManager, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("session_template: resolve path: %w", err)
	}

	m := &SessionTemplateManager{
		templates: make([]SessionTemplate, 0),
		path:      absPath,
	}

	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

// Create adds a new template. It validates that Name and Tool are non-empty,
// checks for duplicate names, sets CreatedAt, and persists to disk.
func (m *SessionTemplateManager) Create(tpl SessionTemplate) error {
	if tpl.Name == "" {
		return fmt.Errorf("session_template: name is required")
	}
	if tpl.Tool == "" {
		return fmt.Errorf("session_template: tool is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.templates {
		if t.Name == tpl.Name {
			return fmt.Errorf("session_template: template %q already exists", tpl.Name)
		}
	}

	tpl.CreatedAt = time.Now().Format(time.RFC3339)
	m.templates = append(m.templates, tpl)
	return m.save()
}

// Get returns the template with the given name, or an error if not found.
func (m *SessionTemplateManager) Get(name string) (*SessionTemplate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.templates {
		if m.templates[i].Name == name {
			tpl := m.templates[i]
			return &tpl, nil
		}
	}
	return nil, fmt.Errorf("session_template: template %q not found", name)
}

// List returns all templates.
func (m *SessionTemplateManager) List() []SessionTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]SessionTemplate, len(m.templates))
	copy(result, m.templates)
	return result
}

// Delete removes the template with the given name and persists to disk.
func (m *SessionTemplateManager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, t := range m.templates {
		if t.Name == name {
			m.templates = append(m.templates[:i], m.templates[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("session_template: template %q not found", name)
}

// ---------------------------------------------------------------------------
// Standalone serialization helpers (import/export & round-trip testing)
// ---------------------------------------------------------------------------

// MarshalTemplate serializes a SessionTemplate to JSON.
func MarshalTemplate(tpl SessionTemplate) ([]byte, error) {
	return json.Marshal(tpl)
}

// UnmarshalTemplate deserializes JSON into a SessionTemplate.
// It returns an error if required fields (Name, Tool) are missing.
func UnmarshalTemplate(data []byte) (SessionTemplate, error) {
	var tpl SessionTemplate
	if err := json.Unmarshal(data, &tpl); err != nil {
		return SessionTemplate{}, fmt.Errorf("session_template: unmarshal: %w", err)
	}
	if tpl.Name == "" {
		return SessionTemplate{}, fmt.Errorf("name is required")
	}
	if tpl.Tool == "" {
		return SessionTemplate{}, fmt.Errorf("tool is required")
	}
	return tpl, nil
}

// ---------------------------------------------------------------------------
// Internal persistence helpers
// ---------------------------------------------------------------------------

// load reads templates from the JSON file on disk. If the file does not exist,
// it starts with an empty slice (not an error).
func (m *SessionTemplateManager) load() error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("session_template: create dir: %w", err)
	}

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // first run, nothing to load
		}
		return fmt.Errorf("session_template: read file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var templates []SessionTemplate
	if err := json.Unmarshal(data, &templates); err != nil {
		return fmt.Errorf("session_template: unmarshal: %w", err)
	}
	m.templates = templates
	return nil
}

// save writes the current templates to disk as JSON.
// Must be called with m.mu held (write lock).
func (m *SessionTemplateManager) save() error {
	data, err := json.MarshalIndent(m.templates, "", "  ")
	if err != nil {
		return fmt.Errorf("session_template: marshal: %w", err)
	}
	if err := os.WriteFile(m.path, data, 0o644); err != nil {
		return fmt.Errorf("session_template: write file: %w", err)
	}
	return nil
}
