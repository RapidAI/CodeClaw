package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/memory"
	"github.com/RapidAI/CodeClaw/hub/internal/nlrouter"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	// candidateStoreKey is the SystemSettingsRepository key for candidate skills.
	candidateStoreKey = "candidate_skills"
	// minPatternLength is the minimum number of consecutive identical actions
	// required to detect a pattern.
	minPatternLength = 3
)

// ActionRecord is a simplified action representation used for pattern detection.
type ActionRecord struct {
	Intent string
	Tool   string
}

// Crystallizer detects repeating action patterns from user history and
// generates candidate Skill definitions for user confirmation.
type Crystallizer struct {
	memory   *memory.Store
	executor *Executor
	context  *nlrouter.ContextWindowManager
	system   store.SystemSettingsRepository
	mu       sync.Mutex
}

// NewCrystallizer creates a new Crystallizer.
func NewCrystallizer(
	mem *memory.Store,
	exec *Executor,
	ctxMgr *nlrouter.ContextWindowManager,
	system store.SystemSettingsRepository,
) *Crystallizer {
	return &Crystallizer{
		memory:   mem,
		executor: exec,
		context:  ctxMgr,
		system:   system,
	}
}

// loadCandidates reads candidate skills from the SystemSettingsRepository.
func (c *Crystallizer) loadCandidates(ctx context.Context) ([]SkillDefinition, error) {
	raw, err := c.system.Get(ctx, candidateStoreKey)
	if err != nil || raw == "" {
		return nil, nil
	}
	var candidates []SkillDefinition
	if err := json.Unmarshal([]byte(raw), &candidates); err != nil {
		return nil, fmt.Errorf("crystallizer: unmarshal candidates: %w", err)
	}
	return candidates, nil
}

// saveCandidates persists candidate skills to the SystemSettingsRepository.
func (c *Crystallizer) saveCandidates(ctx context.Context, candidates []SkillDefinition) error {
	data, err := json.Marshal(candidates)
	if err != nil {
		return fmt.Errorf("crystallizer: marshal candidates: %w", err)
	}
	return c.system.Set(ctx, candidateStoreKey, string(data))
}

// DetectPatterns inspects the user's recent actions from Memory_Store and
// returns candidate SkillDefinitions for any sequence of minPatternLength or
// more consecutive identical intent patterns. Already-existing skills with
// similar steps are excluded (dedup).
func (c *Crystallizer) DetectPatterns(ctx context.Context, userID string) ([]SkillDefinition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	mem, err := c.memory.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("crystallizer: get memory: %w", err)
	}

	actions := mem.RecentActions
	if len(actions) < minPatternLength {
		return nil, nil
	}

	// Detect consecutive runs of the same intent sequence.
	sequences := detectRepeatingSequences(actions)

	var candidates []SkillDefinition
	for _, seq := range sequences {
		candidate := sequenceToCandidate(seq)

		// Dedup: skip if a similar skill already exists.
		if c.hasSimilarSkill(ctx, candidate) {
			continue
		}

		// Dedup: skip if already a candidate.
		existing, _ := c.loadCandidates(ctx)
		if containsCandidate(existing, candidate.Name) {
			continue
		}

		candidates = append(candidates, candidate)
	}

	// Persist new candidates.
	if len(candidates) > 0 {
		existing, _ := c.loadCandidates(ctx)
		existing = append(existing, candidates...)
		if err := c.saveCandidates(ctx, existing); err != nil {
			return nil, err
		}
	}

	return candidates, nil
}

// CrystallizeFromContext extracts the most recent action sequence from the
// Context_Window and Memory_Store for the given user and generates a candidate
// Skill definition. This is triggered when the user says something like
// "把刚才的操作保存为技能".
func (c *Crystallizer) CrystallizeFromContext(ctx context.Context, userID string) (*SkillDefinition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Gather actions from Context_Window entries.
	var entries []memory.MemoryEntry
	if c.context != nil {
		cw := c.context.Get(userID)
		for _, e := range cw.Entries {
			if e.Role == "user" && e.Intent != "" && e.Intent != "crystallize_skill" && e.Intent != "unknown" {
				entries = append(entries, memory.MemoryEntry{
					Intent:    e.Intent,
					Timestamp: e.Timestamp,
				})
			}
		}
	}

	// If context window is sparse, supplement from Memory_Store recent actions.
	if len(entries) < 2 {
		mem, err := c.memory.Get(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("crystallizer: get memory: %w", err)
		}
		// Take the last 5 actions from memory.
		start := len(mem.RecentActions) - 5
		if start < 0 {
			start = 0
		}
		entries = mem.RecentActions[start:]
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("crystallizer: no recent actions found for user %s", userID)
	}

	// Build candidate from the collected entries.
	steps := make([]SkillStep, 0, len(entries))
	intentParts := make([]string, 0, len(entries))
	for _, e := range entries {
		steps = append(steps, SkillStep{
			Action:  e.Intent,
			Params:  e.Params,
			OnError: "stop",
		})
		intentParts = append(intentParts, e.Intent)
	}

	name := fmt.Sprintf("auto-%s", strings.Join(intentParts, "-"))
	if len(name) > 60 {
		name = name[:60]
	}

	candidate := SkillDefinition{
		Name:        name,
		Description: fmt.Sprintf("自动沉淀的技能: %s", strings.Join(intentParts, " → ")),
		Triggers:    []string{name},
		Steps:       steps,
		Status:      "candidate",
		CreatedAt:   time.Now(),
	}

	// Dedup check.
	if c.hasSimilarSkill(ctx, candidate) {
		return &candidate, fmt.Errorf("crystallizer: similar skill already exists")
	}

	// Persist as candidate.
	existing, _ := c.loadCandidates(ctx)
	if !containsCandidate(existing, candidate.Name) {
		existing = append(existing, candidate)
		if err := c.saveCandidates(ctx, existing); err != nil {
			return nil, err
		}
	}

	return &candidate, nil
}

// Confirm promotes a candidate Skill to active status and registers it with
// the Executor. The candidate is removed from the candidates list.
func (c *Crystallizer) Confirm(ctx context.Context, candidate SkillDefinition) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove from candidates list.
	candidates, err := c.loadCandidates(ctx)
	if err != nil {
		return err
	}
	candidates = removeCandidate(candidates, candidate.Name)
	if err := c.saveCandidates(ctx, candidates); err != nil {
		return err
	}

	// Set status to active and register.
	candidate.Status = "active"
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now()
	}
	return c.executor.Register(ctx, candidate)
}

// Ignore removes a candidate Skill from the candidates list without
// registering it.
func (c *Crystallizer) Ignore(ctx context.Context, candidateName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	candidates, err := c.loadCandidates(ctx)
	if err != nil {
		return err
	}
	candidates = removeCandidate(candidates, candidateName)
	return c.saveCandidates(ctx, candidates)
}

// ListCandidates returns all current candidate skills.
func (c *Crystallizer) ListCandidates(ctx context.Context) ([]SkillDefinition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadCandidates(ctx)
}

// hasSimilarSkill checks if the Executor already has a skill with similar
// steps (same action sequence).
func (c *Crystallizer) hasSimilarSkill(_ context.Context, candidate SkillDefinition) bool {
	existing := c.executor.List(context.Background())
	candidateActions := extractActions(candidate.Steps)

	for _, sk := range existing {
		if sk.Status == "disabled" {
			continue
		}
		existingActions := extractActions(sk.Steps)
		if actionsEqual(candidateActions, existingActions) {
			return true
		}
	}
	return false
}

// ---------- helpers ----------

// repeatingSequence represents a detected repeating pattern of actions.
type repeatingSequence struct {
	Actions []memory.MemoryEntry
	Count   int
}

// detectRepeatingSequences scans recent actions for consecutive runs of the
// same intent. A "run" is a contiguous sub-slice where every entry has the
// same intent value. Runs of length >= minPatternLength are returned.
func detectRepeatingSequences(actions []memory.MemoryEntry) []repeatingSequence {
	if len(actions) == 0 {
		return nil
	}

	var results []repeatingSequence

	i := 0
	for i < len(actions) {
		j := i + 1
		for j < len(actions) && actions[j].Intent == actions[i].Intent {
			j++
		}
		runLen := j - i
		if runLen >= minPatternLength {
			results = append(results, repeatingSequence{
				Actions: actions[i:j],
				Count:   runLen,
			})
		}
		i = j
	}

	return results
}

// sequenceToCandidate converts a repeating sequence into a candidate
// SkillDefinition.
func sequenceToCandidate(seq repeatingSequence) SkillDefinition {
	// Use the first entry as the representative step.
	representative := seq.Actions[0]
	step := SkillStep{
		Action:  representative.Intent,
		Params:  representative.Params,
		OnError: "stop",
	}

	name := fmt.Sprintf("auto-%s-x%d", representative.Intent, seq.Count)
	return SkillDefinition{
		Name:        name,
		Description: fmt.Sprintf("自动检测到的重复操作: %s (连续 %d 次)", representative.Intent, seq.Count),
		Triggers:    []string{representative.Intent},
		Steps:       []SkillStep{step},
		Status:      "candidate",
		CreatedAt:   time.Now(),
	}
}

// extractActions returns the action names from a slice of SkillSteps.
func extractActions(steps []SkillStep) []string {
	actions := make([]string, len(steps))
	for i, s := range steps {
		actions[i] = s.Action
	}
	return actions
}

// actionsEqual returns true if two action slices are identical.
func actionsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// containsCandidate returns true if the candidates list already has an entry
// with the given name.
func containsCandidate(candidates []SkillDefinition, name string) bool {
	for _, c := range candidates {
		if c.Name == name {
			return true
		}
	}
	return false
}

// removeCandidate returns a new slice with the named candidate removed.
func removeCandidate(candidates []SkillDefinition, name string) []SkillDefinition {
	result := make([]SkillDefinition, 0, len(candidates))
	for _, c := range candidates {
		if c.Name != name {
			result = append(result, c)
		}
	}
	return result
}
