package memory

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// allCategories is the full set of valid Category values.
var allCategories = []Category{
	CategorySelfIdentity,
	CategoryUserFact,
	CategoryPreference,
	CategoryProjectKnowledge,
	CategoryInstruction,
	CategoryConversationSummary,
	CategorySessionCheckpoint,
}

// genCategory generates a random Category from the valid set.
func genCategory() *rapid.Generator[Category] {
	return rapid.Custom(func(t *rapid.T) Category {
		idx := rapid.IntRange(0, len(allCategories)-1).Draw(t, "catIdx")
		return allCategories[idx]
	})
}

// genNonEmptyString generates a non-empty ASCII string suitable for entry content.
func genNonEmptyString() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		return rapid.StringMatching(`[a-zA-Z0-9 ]{1,100}`).Draw(t, "str")
	})
}

// Feature: memory-claude-style-upgrade, Property 10: Pin/Unpin round-trip
// **Validates: Requirements 4.4, 4.5**
//
// For any entry, PinEntry sets Pinned=true, UnpinEntry sets Pinned=false,
// other fields unchanged.
func TestProperty_PinUnpinRoundTrip(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(t, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Stop()

		content := genNonEmptyString().Draw(t, "content")
		cat := genCategory().Draw(t, "category")
		tags := []string{rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "tag")}

		entry := Entry{
			Content:  content,
			Category: cat,
			Tags:     tags,
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}

		// Get the saved entry's ID.
		store.mu.RLock()
		saved := store.entries[0]
		store.mu.RUnlock()

		// Snapshot fields before pin.
		beforeContent := saved.Content
		beforeCategory := saved.Category
		beforeAccessCount := saved.AccessCount

		// Pin the entry.
		if err := store.PinEntry(saved.ID); err != nil {
			t.Fatalf("PinEntry failed: %v", err)
		}

		store.mu.RLock()
		pinned := store.entries[0]
		store.mu.RUnlock()

		if !pinned.Pinned {
			t.Fatal("expected Pinned=true after PinEntry")
		}
		if pinned.Content != beforeContent {
			t.Fatalf("Content changed after PinEntry: %q -> %q", beforeContent, pinned.Content)
		}
		if pinned.Category != beforeCategory {
			t.Fatalf("Category changed after PinEntry: %q -> %q", beforeCategory, pinned.Category)
		}
		if pinned.AccessCount != beforeAccessCount {
			t.Fatalf("AccessCount changed after PinEntry: %d -> %d", beforeAccessCount, pinned.AccessCount)
		}

		// Unpin the entry.
		if err := store.UnpinEntry(saved.ID); err != nil {
			t.Fatalf("UnpinEntry failed: %v", err)
		}

		store.mu.RLock()
		unpinned := store.entries[0]
		store.mu.RUnlock()

		if unpinned.Pinned {
			t.Fatal("expected Pinned=false after UnpinEntry")
		}
		if unpinned.Content != beforeContent {
			t.Fatalf("Content changed after UnpinEntry: %q -> %q", beforeContent, unpinned.Content)
		}
		if unpinned.Category != beforeCategory {
			t.Fatalf("Category changed after UnpinEntry: %q -> %q", beforeCategory, unpinned.Category)
		}
		if unpinned.AccessCount != beforeAccessCount {
			t.Fatalf("AccessCount changed after UnpinEntry: %d -> %d", beforeAccessCount, unpinned.AccessCount)
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 14: Any category can be pinned
// **Validates: Requirements 4.8**
//
// For any category, creating an entry and calling PinEntry should succeed.
func TestProperty_AnyCategoryPin(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(t, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Stop()

		cat := genCategory().Draw(t, "category")
		content := genNonEmptyString().Draw(t, "content")

		entry := Entry{
			Content:  content,
			Category: cat,
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}

		store.mu.RLock()
		id := store.entries[0].ID
		store.mu.RUnlock()

		if err := store.PinEntry(id); err != nil {
			t.Fatalf("PinEntry failed for category %q: %v", cat, err)
		}

		store.mu.RLock()
		isPinned := store.entries[0].Pinned
		store.mu.RUnlock()

		if !isPinned {
			t.Fatalf("expected Pinned=true for category %q", cat)
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 18: Backward compatible deserialization
// **Validates: Requirements 6.1**
//
// JSON without "pinned" field loads with Pinned==false.
func TestProperty_BackwardCompatDeserialize(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Build a JSON entry without the "pinned" field.
		content := genNonEmptyString().Draw(t, "content")
		cat := genCategory().Draw(t, "category")
		now := time.Now().Truncate(time.Second)

		// Use a raw map to construct JSON without "pinned".
		raw := map[string]interface{}{
			"id":           "test-" + rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, "id"),
			"content":      content,
			"category":     string(cat),
			"tags":         []string{"compat-test"},
			"created_at":   now.Format(time.RFC3339),
			"updated_at":   now.Format(time.RFC3339),
			"access_count": 1,
		}

		data, err := json.Marshal([]interface{}{raw})
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var entries []Entry
		if err := json.Unmarshal(data, &entries); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		if entries[0].Pinned {
			t.Fatal("expected Pinned==false for JSON without pinned field")
		}
		if entries[0].Content != content {
			t.Fatalf("content mismatch: %q != %q", entries[0].Content, content)
		}
		if entries[0].Category != cat {
			t.Fatalf("category mismatch: %q != %q", entries[0].Category, cat)
		}
	})
}
