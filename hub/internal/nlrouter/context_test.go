package nlrouter

import (
	"testing"
	"time"
)

func TestNewContextWindowManager(t *testing.T) {
	m := NewContextWindowManager()
	if m == nil {
		t.Fatal("NewContextWindowManager returned nil")
	}
	if m.windows == nil {
		t.Fatal("windows map not initialized")
	}
}

func TestGet_CreatesNewWindow(t *testing.T) {
	m := NewContextWindowManager()
	w := m.Get("user1")
	if w == nil {
		t.Fatal("Get returned nil for new user")
	}
	if len(w.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(w.Entries))
	}
	if w.ActiveSession != "" {
		t.Errorf("expected empty ActiveSession, got %q", w.ActiveSession)
	}
	if w.ActiveTool != "" {
		t.Errorf("expected empty ActiveTool, got %q", w.ActiveTool)
	}
}

func TestGet_ReturnsSameWindow(t *testing.T) {
	m := NewContextWindowManager()
	w1 := m.Get("user1")
	w1.ActiveSession = "session-abc"
	w2 := m.Get("user1")
	if w2.ActiveSession != "session-abc" {
		t.Errorf("expected same window, ActiveSession mismatch")
	}
}

func TestGet_ClearsExpiredWindow(t *testing.T) {
	m := NewContextWindowManager()

	// Manually insert an expired window
	m.mu.Lock()
	m.windows["user1"] = &ContextWindow{
		Entries:        []ContextEntry{{Role: "user", Text: "old"}},
		ActiveSession:  "old-session",
		ActiveTool:     "old-tool",
		LastActivityAt: time.Now().Add(-31 * time.Minute),
	}
	m.mu.Unlock()

	w := m.Get("user1")
	if len(w.Entries) != 0 {
		t.Errorf("expected expired window to be cleared, got %d entries", len(w.Entries))
	}
	if w.ActiveSession != "" {
		t.Errorf("expected ActiveSession cleared, got %q", w.ActiveSession)
	}
	if w.ActiveTool != "" {
		t.Errorf("expected ActiveTool cleared, got %q", w.ActiveTool)
	}
}

func TestAdd_AppendsEntry(t *testing.T) {
	m := NewContextWindowManager()
	entry := ContextEntry{
		Role:      "user",
		Text:      "hello",
		Intent:    "help",
		Timestamp: time.Now(),
	}
	m.Add("user1", entry)

	w := m.Get("user1")
	if len(w.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.Entries))
	}
	if w.Entries[0].Text != "hello" {
		t.Errorf("expected text 'hello', got %q", w.Entries[0].Text)
	}
}

func TestAdd_TrimsToMaxEntries(t *testing.T) {
	m := NewContextWindowManager()

	// Add 12 entries
	for i := 0; i < 12; i++ {
		m.Add("user1", ContextEntry{
			Role:      "user",
			Text:      string(rune('a' + i)),
			Intent:    "test",
			Timestamp: time.Now(),
		})
	}

	w := m.Get("user1")
	if len(w.Entries) != MaxContextEntries {
		t.Errorf("expected %d entries, got %d", MaxContextEntries, len(w.Entries))
	}
	// The oldest 2 entries ('a' and 'b') should be trimmed
	// First remaining entry should be 'c' (index 2)
	if w.Entries[0].Text != string(rune('c')) {
		t.Errorf("expected first entry text 'c', got %q", w.Entries[0].Text)
	}
}

func TestAdd_UpdatesLastActivityAt(t *testing.T) {
	m := NewContextWindowManager()

	before := time.Now()
	m.Add("user1", ContextEntry{Role: "user", Text: "test", Timestamp: time.Now()})
	after := time.Now()

	w := m.Get("user1")
	if w.LastActivityAt.Before(before) || w.LastActivityAt.After(after) {
		t.Errorf("LastActivityAt not updated correctly")
	}
}

func TestAdd_ClearsExpiredBeforeAdding(t *testing.T) {
	m := NewContextWindowManager()

	// Insert an expired window with existing entries
	m.mu.Lock()
	m.windows["user1"] = &ContextWindow{
		Entries:        []ContextEntry{{Role: "user", Text: "old"}},
		ActiveSession:  "old-session",
		ActiveTool:     "old-tool",
		LastActivityAt: time.Now().Add(-31 * time.Minute),
	}
	m.mu.Unlock()

	m.Add("user1", ContextEntry{Role: "user", Text: "new", Timestamp: time.Now()})

	w := m.Get("user1")
	if len(w.Entries) != 1 {
		t.Errorf("expected 1 entry after expired clear+add, got %d", len(w.Entries))
	}
	if w.Entries[0].Text != "new" {
		t.Errorf("expected entry text 'new', got %q", w.Entries[0].Text)
	}
	if w.ActiveSession != "" {
		t.Errorf("expected ActiveSession cleared, got %q", w.ActiveSession)
	}
}

func TestClear_RemovesWindow(t *testing.T) {
	m := NewContextWindowManager()
	m.Add("user1", ContextEntry{Role: "user", Text: "test", Timestamp: time.Now()})

	m.Clear("user1")

	// After clear, Get should return a fresh window
	w := m.Get("user1")
	if len(w.Entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(w.Entries))
	}
}

func TestClear_NonExistentUser(t *testing.T) {
	m := NewContextWindowManager()
	// Should not panic
	m.Clear("nonexistent")
}

func TestIsExpired_NoWindow(t *testing.T) {
	m := NewContextWindowManager()
	if !m.IsExpired("nonexistent") {
		t.Error("expected IsExpired to return true for nonexistent user")
	}
}

func TestIsExpired_FreshWindow(t *testing.T) {
	m := NewContextWindowManager()
	m.Add("user1", ContextEntry{Role: "user", Text: "test", Timestamp: time.Now()})

	if m.IsExpired("user1") {
		t.Error("expected IsExpired to return false for fresh window")
	}
}

func TestIsExpired_ExpiredWindow(t *testing.T) {
	m := NewContextWindowManager()

	m.mu.Lock()
	m.windows["user1"] = &ContextWindow{
		Entries:        []ContextEntry{{Role: "user", Text: "old"}},
		LastActivityAt: time.Now().Add(-31 * time.Minute),
	}
	m.mu.Unlock()

	if !m.IsExpired("user1") {
		t.Error("expected IsExpired to return true for expired window")
	}
}

func TestConstants(t *testing.T) {
	if MaxContextEntries != 10 {
		t.Errorf("expected MaxContextEntries=10, got %d", MaxContextEntries)
	}
	if ContextTimeout != 30*time.Minute {
		t.Errorf("expected ContextTimeout=30m, got %v", ContextTimeout)
	}
}
