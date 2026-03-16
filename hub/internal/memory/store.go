package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	// MaxRecentActions is the maximum number of recent actions to keep per user.
	MaxRecentActions = 500
	// ConsecutiveThreshold is the number of consecutive same-tool usages
	// required to auto-set the default tool preference.
	ConsecutiveThreshold = 3
)

// MemoryEntry represents a single recorded action.
type MemoryEntry struct {
	Intent    string                 `json:"intent"`
	Params    map[string]interface{} `json:"params"`
	Timestamp time.Time              `json:"timestamp"`
}

// UserMemory holds the memory space for a single user.
type UserMemory struct {
	UserID         string            `json:"user_id"`
	DefaultTool    string            `json:"default_tool"`
	Preferences    map[string]string `json:"preferences"`
	RecentActions  []MemoryEntry     `json:"recent_actions"`
	ActionPatterns map[string]int    `json:"action_patterns"`
}

// Store provides persistent user memory backed by SystemSettingsRepository.
type Store struct {
	system store.SystemSettingsRepository
	mu     sync.RWMutex
	cache  map[string]*UserMemory
}

// NewStore creates a new memory Store.
func NewStore(system store.SystemSettingsRepository) *Store {
	return &Store{
		system: system,
		cache:  make(map[string]*UserMemory),
	}
}

// storeKey returns the SystemSettingsRepository key for a given user.
func storeKey(userID string) string {
	return fmt.Sprintf("memory_%s", userID)
}

// newUserMemory creates an empty UserMemory for the given user.
func newUserMemory(userID string) *UserMemory {
	return &UserMemory{
		UserID:         userID,
		Preferences:    make(map[string]string),
		RecentActions:  make([]MemoryEntry, 0),
		ActionPatterns: make(map[string]int),
	}
}

// Get retrieves the user memory from cache or DB.
// If no memory exists yet, an empty UserMemory is returned.
func (s *Store) Get(ctx context.Context, userID string) (*UserMemory, error) {
	s.mu.RLock()
	if mem, ok := s.cache[userID]; ok {
		s.mu.RUnlock()
		return mem, nil
	}
	s.mu.RUnlock()

	// Load from DB.
	raw, err := s.system.Get(ctx, storeKey(userID))
	if err != nil || raw == "" {
		mem := newUserMemory(userID)
		s.mu.Lock()
		s.cache[userID] = mem
		s.mu.Unlock()
		return mem, nil
	}

	var mem UserMemory
	if err := json.Unmarshal([]byte(raw), &mem); err != nil {
		return nil, fmt.Errorf("memory: unmarshal user %s: %w", userID, err)
	}
	if mem.Preferences == nil {
		mem.Preferences = make(map[string]string)
	}
	if mem.ActionPatterns == nil {
		mem.ActionPatterns = make(map[string]int)
	}
	if mem.RecentActions == nil {
		mem.RecentActions = make([]MemoryEntry, 0)
	}

	s.mu.Lock()
	s.cache[userID] = &mem
	s.mu.Unlock()
	return &mem, nil
}

// RecordAction appends an action entry to the user's memory, updates pattern
// counts, checks for consecutive tool usage, trims to MaxRecentActions, and
// persists the result.
func (s *Store) RecordAction(ctx context.Context, userID string, entry MemoryEntry) error {
	mem, err := s.Get(ctx, userID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Append the new entry.
	mem.RecentActions = append(mem.RecentActions, entry)

	// Update action pattern count using "intent:tool" as key when applicable.
	patternKey := entry.Intent
	if tool, ok := entry.Params["tool"]; ok {
		if ts, ok := tool.(string); ok && ts != "" {
			patternKey = fmt.Sprintf("%s:%s", entry.Intent, ts)
		}
	}
	mem.ActionPatterns[patternKey]++

	// Check consecutive tool usage for launch_session intent.
	s.checkConsecutiveToolUsage(mem)

	// Trim to MaxRecentActions — keep the most recent entries.
	if len(mem.RecentActions) > MaxRecentActions {
		mem.RecentActions = mem.RecentActions[len(mem.RecentActions)-MaxRecentActions:]
	}

	return s.persistLocked(ctx, mem)
}

// checkConsecutiveToolUsage inspects the last ConsecutiveThreshold entries in
// RecentActions. If all are launch_session intents with the same tool param,
// the DefaultTool is updated.
func (s *Store) checkConsecutiveToolUsage(mem *UserMemory) {
	n := len(mem.RecentActions)
	if n < ConsecutiveThreshold {
		return
	}

	var tool string
	for i := n - ConsecutiveThreshold; i < n; i++ {
		e := mem.RecentActions[i]
		if e.Intent != "launch_session" {
			return
		}
		t, ok := e.Params["tool"]
		if !ok {
			return
		}
		ts, ok := t.(string)
		if !ok || ts == "" {
			return
		}
		if tool == "" {
			tool = ts
		} else if tool != ts {
			return
		}
	}

	if tool != "" {
		mem.DefaultTool = tool
	}
}

// GetDefaultTool returns the user's default tool preference.
func (s *Store) GetDefaultTool(ctx context.Context, userID string) string {
	mem, err := s.Get(ctx, userID)
	if err != nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return mem.DefaultTool
}

// Clear removes the user's memory from cache and persists an empty state.
func (s *Store) Clear(ctx context.Context, userID string) error {
	s.mu.Lock()
	delete(s.cache, userID)
	s.mu.Unlock()

	empty := newUserMemory(userID)
	data, err := json.Marshal(empty)
	if err != nil {
		return fmt.Errorf("memory: marshal empty for %s: %w", userID, err)
	}
	return s.system.Set(ctx, storeKey(userID), string(data))
}

// persistLocked marshals and saves the user memory to the DB.
// Caller must hold s.mu (at least read lock on mem).
func (s *Store) persistLocked(ctx context.Context, mem *UserMemory) error {
	data, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("memory: marshal user %s: %w", mem.UserID, err)
	}
	return s.system.Set(ctx, storeKey(mem.UserID), string(data))
}
