package memory

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// Feature: memory-claude-style-upgrade, Property 1: Tag preservation round-trip
// **Validates: Requirements 1.3, 2.5**
//
// For any Entry saved to the Memory_Store with a non-empty tag list
// (including tags like "proactive" or "extracted"), listing or searching
// the store should return that entry with all original tags intact and
// unmodified.
func TestProperty_TagPreservation(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Generate 1-5 entries with random tags.
		n := rapid.IntRange(1, 5).Draw(rt, "entryCount")
		type savedEntry struct {
			content string
			cat     Category
			tags    []string
		}
		var saved []savedEntry

		for i := 0; i < n; i++ {
			content := rapid.StringMatching(`[a-zA-Z0-9]{5,80}`).Draw(rt, fmt.Sprintf("content_%d", i))
			cat := genCategory().Draw(rt, fmt.Sprintf("cat_%d", i))

			// Generate 1-4 tags, including special tags like "proactive" and "extracted".
			specialTags := []string{"proactive", "extracted"}
			numTags := rapid.IntRange(1, 4).Draw(rt, fmt.Sprintf("numTags_%d", i))
			tags := make([]string, 0, numTags)
			for j := 0; j < numTags; j++ {
				useSpecial := rapid.Bool().Draw(rt, fmt.Sprintf("special_%d_%d", i, j))
				if useSpecial && len(specialTags) > 0 {
					idx := rapid.IntRange(0, len(specialTags)-1).Draw(rt, fmt.Sprintf("specialIdx_%d_%d", i, j))
					tags = append(tags, specialTags[idx])
				} else {
					tag := rapid.StringMatching(`[a-z]{2,15}`).Draw(rt, fmt.Sprintf("tag_%d_%d", i, j))
					tags = append(tags, tag)
				}
			}
			// Deduplicate tags to avoid confusion in comparison.
			tags = uniqueTags(tags)

			entry := Entry{
				Content:  content,
				Category: cat,
				Tags:     tags,
			}
			if err := store.Save(entry); err != nil {
				rt.Fatal(err)
			}
			saved = append(saved, savedEntry{content: content, cat: cat, tags: tags})
		}

		// Verify via List: each saved entry's tags are preserved.
		listed := store.List("", "")
		for _, se := range saved {
			found := false
			for _, e := range listed {
				if e.Content == se.content {
					found = true
					if !tagsEqual(e.Tags, se.tags) {
						rt.Fatalf("tag mismatch for entry %q:\n  saved:  %v\n  listed: %v", se.content, se.tags, e.Tags)
					}
					break
				}
			}
			if !found {
				rt.Fatalf("entry with content %q not found in list output", se.content)
			}
		}

		// Verify via Search: tags also preserved.
		searched := store.Search("", "", 100)
		for _, se := range saved {
			for _, e := range searched {
				if e.Content == se.content {
					if !tagsEqual(e.Tags, se.tags) {
						rt.Fatalf("tag mismatch in search for entry %q:\n  saved:  %v\n  search: %v", se.content, se.tags, e.Tags)
					}
					break
				}
			}
		}
	})
}

// uniqueTags deduplicates a tag slice preserving order.
func uniqueTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// tagsEqual checks if two tag slices contain the same elements (order-insensitive).
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := make([]string, len(a))
	copy(sa, a)
	sort.Strings(sa)
	sb := make([]string, len(b))
	copy(sb, b)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
