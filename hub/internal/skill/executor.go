package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/discovery"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	// storeKey is the SystemSettingsRepository key for persisting skills.
	storeKey = "skills"
)

// SkillDefinition is a YAML-defined skill containing a sequence of steps.
type SkillDefinition struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Triggers    []string    `yaml:"triggers" json:"triggers"`
	Steps       []SkillStep `yaml:"steps" json:"steps"`
	Status      string      `json:"status"` // "active", "candidate", "disabled"
	CreatedAt   time.Time   `json:"created_at"`
}

// SkillStep represents a single action within a skill.
type SkillStep struct {
	Action  string                 `yaml:"action" json:"action"`
	Params  map[string]interface{} `yaml:"params" json:"params"`
	OnError string                 `yaml:"on_error" json:"on_error"` // "stop", "skip", "retry"
}

// ActionHandler is the interface that the IM Adapter provides to execute
// individual skill step actions. The Executor delegates all action execution
// to this handler rather than calling MCP or session manager directly.
type ActionHandler interface {
	HandleAction(ctx context.Context, action string, params map[string]interface{}) error
}

// Executor manages skill registration, persistence, and sequential execution.
type Executor struct {
	store     store.SystemSettingsRepository
	skills    map[string]*SkillDefinition
	mu        sync.RWMutex
	discovery *discovery.Protocol
	handler   ActionHandler
}

// NewExecutor creates a new Executor and loads persisted skills from the DB.
func NewExecutor(system store.SystemSettingsRepository, disc *discovery.Protocol, handler ActionHandler) (*Executor, error) {
	e := &Executor{
		store:     system,
		skills:    make(map[string]*SkillDefinition),
		discovery: disc,
		handler:   handler,
	}
	if err := e.load(context.Background()); err != nil {
		return nil, fmt.Errorf("skill: load skills: %w", err)
	}
	// Re-register all loaded skills in discovery.
	for _, sk := range e.skills {
		e.registerInDiscovery(sk)
	}
	return e, nil
}

// SetActionHandler replaces the current ActionHandler. This is useful when
// the handler is not available at construction time.
func (e *Executor) SetActionHandler(handler ActionHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handler = handler
}

// load reads persisted skills from the SystemSettingsRepository.
func (e *Executor) load(ctx context.Context) error {
	raw, err := e.store.Get(ctx, storeKey)
	if err != nil || raw == "" {
		return nil
	}
	var skills []SkillDefinition
	if err := json.Unmarshal([]byte(raw), &skills); err != nil {
		return fmt.Errorf("skill: unmarshal skills: %w", err)
	}
	for i := range skills {
		sk := skills[i]
		e.skills[sk.Name] = &sk
	}
	return nil
}

// persist saves all skills to the SystemSettingsRepository. Caller must hold e.mu.
func (e *Executor) persist(ctx context.Context) error {
	skills := make([]SkillDefinition, 0, len(e.skills))
	for _, sk := range e.skills {
		skills = append(skills, *sk)
	}
	data, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("skill: marshal skills: %w", err)
	}
	return e.store.Set(ctx, storeKey, string(data))
}

// skillIndexName returns the discovery index name for a skill.
func skillIndexName(skillName string) string {
	return fmt.Sprintf("skill:%s", skillName)
}

// registerInDiscovery registers a skill in the discovery Protocol.
func (e *Executor) registerInDiscovery(sk *SkillDefinition) {
	tags := []string{"skill", sk.Name}
	tags = append(tags, sk.Triggers...)
	idx := discovery.ToolIndex{
		Name:        skillIndexName(sk.Name),
		Category:    "Skill",
		Description: sk.Description,
		Tags:        tags,
		Source:      "skill",
		Available:   sk.Status != "disabled",
	}
	e.discovery.UpdateIndex(idx)
}

// removeFromDiscovery removes a skill from the discovery Protocol.
func (e *Executor) removeFromDiscovery(skillName string) {
	e.discovery.RemoveIndex(skillIndexName(skillName))
}

// Register adds a new skill, persists it, and registers it in discovery.
func (e *Executor) Register(ctx context.Context, skill SkillDefinition) error {
	if skill.Name == "" {
		return fmt.Errorf("skill: name is required")
	}
	if skill.Status == "" {
		skill.Status = "active"
	}
	if skill.CreatedAt.IsZero() {
		skill.CreatedAt = time.Now()
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.skills[skill.Name] = &skill
	if err := e.persist(ctx); err != nil {
		delete(e.skills, skill.Name)
		return err
	}
	e.registerInDiscovery(&skill)
	return nil
}

// Delete removes a skill, persists the change, and removes it from discovery.
func (e *Executor) Delete(ctx context.Context, skillName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.skills[skillName]; !ok {
		return fmt.Errorf("skill: %s not found", skillName)
	}

	e.removeFromDiscovery(skillName)
	delete(e.skills, skillName)
	return e.persist(ctx)
}

// List returns a copy of all registered skills.
func (e *Executor) List(ctx context.Context) []SkillDefinition {
	e.mu.RLock()
	defer e.mu.RUnlock()

	skills := make([]SkillDefinition, 0, len(e.skills))
	for _, sk := range e.skills {
		skills = append(skills, *sk)
	}
	return skills
}

// Get returns a single skill by name, or nil if not found.
func (e *Executor) Get(ctx context.Context, skillName string) *SkillDefinition {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sk, ok := e.skills[skillName]
	if !ok {
		return nil
	}
	copy := *sk
	return &copy
}

// Execute runs a skill's steps sequentially. After each step completes,
// progressFn is called with the step index and a status message.
// On error, the step's OnError policy is applied:
//   - "stop" (default): halt execution and return the error
//   - "skip": log the error via progressFn and continue to the next step
//   - "retry": retry the step once; if it fails again, halt execution
func (e *Executor) Execute(ctx context.Context, skillName string, params map[string]interface{}, progressFn func(step int, msg string)) error {
	e.mu.RLock()
	sk, ok := e.skills[skillName]
	if !ok {
		e.mu.RUnlock()
		return fmt.Errorf("skill: %s not found", skillName)
	}
	// Copy the skill so we can release the lock.
	def := *sk
	handler := e.handler
	e.mu.RUnlock()

	if handler == nil {
		return fmt.Errorf("skill: no action handler configured")
	}

	for i, step := range def.Steps {
		// Merge skill-level params into step params (step params take precedence).
		merged := mergeParams(params, step.Params)

		err := handler.HandleAction(ctx, step.Action, merged)
		if err != nil {
			switch step.OnError {
			case "skip":
				if progressFn != nil {
					progressFn(i, fmt.Sprintf("步骤 %d (%s) 失败已跳过: %v", i+1, step.Action, err))
				}
				continue
			case "retry":
				// Retry once.
				err2 := handler.HandleAction(ctx, step.Action, merged)
				if err2 != nil {
					if progressFn != nil {
						progressFn(i, fmt.Sprintf("步骤 %d (%s) 重试后仍失败: %v", i+1, step.Action, err2))
					}
					return fmt.Errorf("skill: step %d (%s) failed after retry: %w", i+1, step.Action, err2)
				}
			default: // "stop" or unrecognized
				if progressFn != nil {
					progressFn(i, fmt.Sprintf("步骤 %d (%s) 失败: %v", i+1, step.Action, err))
				}
				return fmt.Errorf("skill: step %d (%s) failed: %w", i+1, step.Action, err)
			}
		}

		if progressFn != nil {
			progressFn(i, fmt.Sprintf("步骤 %d (%s) 完成", i+1, step.Action))
		}
	}

	return nil
}

// mergeParams creates a new map combining base params with step-specific params.
// Step params take precedence over base params.
func mergeParams(base, step map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range step {
		merged[k] = v
	}
	return merged
}
