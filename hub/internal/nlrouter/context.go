package nlrouter

import (
	"sync"
	"time"
)

const (
	// MaxContextEntries is the maximum number of entries in a context window.
	MaxContextEntries = 10
	// ContextTimeout is the duration after which a context window expires.
	ContextTimeout = 30 * time.Minute
)

// ContextEntry represents a single entry in the context window.
type ContextEntry struct {
	Role      string    `json:"role"`      // "user" or "system"
	Text      string    `json:"text"`
	Intent    string    `json:"intent"`
	Timestamp time.Time `json:"timestamp"`
}

// ContextWindow holds the conversation context for a single user.
type ContextWindow struct {
	Entries        []ContextEntry // max 10
	ActiveSession  string         // current session ID
	ActiveTool     string         // current tool name
	LastActivityAt time.Time
}

// ContextWindowManager manages per-user context windows with thread safety.
type ContextWindowManager struct {
	windows map[string]*ContextWindow // userID → window
	mu      sync.RWMutex
}

// NewContextWindowManager creates a new ContextWindowManager.
func NewContextWindowManager() *ContextWindowManager {
	return &ContextWindowManager{
		windows: make(map[string]*ContextWindow),
	}
}

// Get returns the context window for the given user.
// If the window does not exist, a new empty one is created.
// If the window has expired (30 min since last activity), it is cleared first.
func (m *ContextWindowManager) Get(userID string) *ContextWindow {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.windows[userID]
	if !ok {
		w = &ContextWindow{
			Entries:        make([]ContextEntry, 0),
			LastActivityAt: time.Now(),
		}
		m.windows[userID] = w
		return w
	}

	// Auto-clear expired windows
	if time.Since(w.LastActivityAt) > ContextTimeout {
		w.Entries = make([]ContextEntry, 0)
		w.ActiveSession = ""
		w.ActiveTool = ""
		w.LastActivityAt = time.Now()
	}

	return w
}

// Add appends an entry to the user's context window.
// If the window exceeds MaxContextEntries, the oldest entries are trimmed.
// LastActivityAt is updated on every add.
func (m *ContextWindowManager) Add(userID string, entry ContextEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.windows[userID]
	if !ok {
		w = &ContextWindow{
			Entries: make([]ContextEntry, 0, MaxContextEntries),
		}
		m.windows[userID] = w
	}

	// Auto-clear expired windows before adding
	if ok && time.Since(w.LastActivityAt) > ContextTimeout {
		w.Entries = make([]ContextEntry, 0, MaxContextEntries)
		w.ActiveSession = ""
		w.ActiveTool = ""
	}

	w.Entries = append(w.Entries, entry)

	// Trim to max 10 entries, keeping the most recent
	if len(w.Entries) > MaxContextEntries {
		w.Entries = w.Entries[len(w.Entries)-MaxContextEntries:]
	}

	w.LastActivityAt = time.Now()
}

// Clear removes the context window for the given user.
func (m *ContextWindowManager) Clear(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.windows, userID)
}

// IsExpired returns true if the user's context window has expired
// (30 minutes since last activity), or if no window exists.
func (m *ContextWindowManager) IsExpired(userID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w, ok := m.windows[userID]
	if !ok {
		return true
	}

	return time.Since(w.LastActivityAt) > ContextTimeout
}
