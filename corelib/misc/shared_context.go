package misc

import (
	"sync"
	"time"
)

// ContextEntry represents a single entry in the shared context store.
type ContextEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

// SharedContextStore provides a thread-safe, size-bounded key-value store
// for sharing context across orchestrator sessions. When the total size
// exceeds maxSize (100KB), the oldest entries are evicted in FIFO order.
type SharedContextStore struct {
	mu      sync.RWMutex
	entries []ContextEntry
	maxSize int
}

// NewSharedContextStore creates a SharedContextStore with a 100KB limit.
func NewSharedContextStore() *SharedContextStore {
	return &SharedContextStore{maxSize: 100 * 1024}
}

func entrySize(e ContextEntry) int {
	return len(e.Key) + len(e.Value) + len(e.SessionID)
}

func (s *SharedContextStore) totalSize() int {
	total := 0
	for _, e := range s.entries {
		total += entrySize(e)
	}
	return total
}

// Put writes a context entry, evicting oldest entries if over size limit.
func (s *SharedContextStore) Put(entry ContextEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	total := s.totalSize()
	for len(s.entries) > 1 && total > s.maxSize {
		total -= entrySize(s.entries[0])
		s.entries = s.entries[1:]
	}
}

// GetForSession returns all context entries for the given sessionID.
func (s *SharedContextStore) GetForSession(sessionID string) []ContextEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []ContextEntry
	for _, e := range s.entries {
		if e.SessionID == sessionID {
			result = append(result, e)
		}
	}
	return result
}
