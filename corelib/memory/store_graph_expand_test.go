package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRecallForProject_GraphExpand verifies that 1-hop graph expansion
// brings in related entries that weren't in the initial top-N.
func TestRecallForProject_GraphExpand(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	// Use deterministic embedder so we can control similarity.
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0}  // high cosine with A
	vecC := []float32{0, 1, 0, 0}        // orthogonal to A and B
	vecQ := []float32{0.95, 0.05, 0, 0}  // query: similar to A and B

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"golang concurrency patterns":  vecA,
			"go goroutine best practices":  vecB,
			"unrelated cooking topic":      vecC,
			"golang parallel programming":  vecQ,
		},
	}
	store.embedder = emb

	// Save entry A — will be a direct match for the query.
	if err := store.Save(Entry{
		Content:   "golang concurrency patterns",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	// Save entry C — unrelated, won't match query.
	if err := store.Save(Entry{
		Content:   "unrelated cooking topic",
		Category:  CategoryProjectKnowledge,
		Embedding: vecC,
	}); err != nil {
		t.Fatal(err)
	}

	// Save entry B — similar to A, autoLink will create a graph edge A↔B.
	if err := store.Save(Entry{
		Content:   "go goroutine best practices",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	// Verify graph edge exists between A and B.
	store.mu.RLock()
	var idA, idB string
	for _, e := range store.entries {
		switch e.Content {
		case "golang concurrency patterns":
			idA = e.ID
		case "go goroutine best practices":
			idB = e.ID
		}
	}
	store.mu.RUnlock()

	if idA == "" || idB == "" {
		t.Fatal("entries not found")
	}

	neighbors := store.graph.neighborsOf(idA)
	if _, ok := neighbors[idB]; !ok {
		t.Fatalf("expected graph edge A→B, got neighbors: %v", neighbors)
	}

	// Recall with a query similar to A — should get both A and B via graph expansion.
	results := store.RecallForProject("golang parallel programming", "")

	foundA, foundB := false, false
	for _, e := range results {
		if e.ID == idA {
			foundA = true
		}
		if e.ID == idB {
			foundB = true
		}
	}

	if !foundA {
		t.Error("expected entry A in recall results")
	}
	if !foundB {
		t.Error("expected entry B in recall results (via graph expansion)")
	}
}

// TestRecallDynamic_GraphExpand verifies graph expansion in RecallDynamic.
func TestRecallDynamic_GraphExpand(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0}
	vecC := []float32{0, 1, 0, 0}
	vecQ := []float32{0.95, 0.05, 0, 0}

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"memory management in go":     vecA,
			"garbage collection tuning":   vecB,
			"unrelated music theory":      vecC,
			"go memory allocation":        vecQ,
		},
	}
	store.embedder = emb

	if err := store.Save(Entry{
		Content:   "memory management in go",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:   "unrelated music theory",
		Category:  CategoryProjectKnowledge,
		Embedding: vecC,
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Entry{
		Content:   "garbage collection tuning",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	// Verify graph edge.
	store.mu.RLock()
	var idA, idB string
	for _, e := range store.entries {
		switch e.Content {
		case "memory management in go":
			idA = e.ID
		case "garbage collection tuning":
			idB = e.ID
		}
	}
	store.mu.RUnlock()

	if idA == "" || idB == "" {
		t.Fatal("entries not found")
	}

	neighbors := store.graph.neighborsOf(idA)
	if _, ok := neighbors[idB]; !ok {
		t.Fatalf("expected graph edge A→B, got neighbors: %v", neighbors)
	}

	results := store.RecallDynamic("go memory allocation", "", "")

	foundA, foundB := false, false
	for _, e := range results {
		if e.ID == idA {
			foundA = true
		}
		if e.ID == idB {
			foundB = true
		}
	}

	if !foundA {
		t.Error("expected entry A in RecallDynamic results")
	}
	if !foundB {
		t.Error("expected entry B in RecallDynamic results (via graph expansion)")
	}
}

// TestGraphExpand_EmptyCandidates verifies graphExpand handles empty input.
func TestGraphExpand_EmptyCandidates(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.mu.RLock()
	result := store.graphExpand(nil, 5)
	store.mu.RUnlock()

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

// TestGraphExpand_NoGraphEdges verifies graphExpand is a no-op when graph is empty.
func TestGraphExpand_NoGraphEdges(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Now()
	entry := Entry{
		ID:        "test-1",
		Content:   "some content",
		Category:  CategoryProjectKnowledge,
		CreatedAt: now,
		UpdatedAt: now,
		Strength:  1.0,
	}

	candidates := []recallScored{{entry: entry, score: 5.0}}

	store.mu.RLock()
	result := store.graphExpand(candidates, 5)
	store.mu.RUnlock()

	if len(result) != 1 {
		t.Errorf("expected 1 candidate (unchanged), got %d", len(result))
	}
}

// TestGraphExpand_Deduplication verifies expanded entries don't duplicate existing candidates.
func TestGraphExpand_Deduplication(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.98, 0.1, 0, 0}

	emb := &deterministicEmbedder{
		dim: 4,
		vectors: map[string][]float32{
			"topic alpha": vecA,
			"topic beta":  vecB,
		},
	}
	store.embedder = emb

	// Save both entries — autoLink will create edge.
	if err := store.Save(Entry{
		Content:   "topic alpha",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:   "topic beta",
		Category:  CategoryProjectKnowledge,
		Embedding: vecB,
	}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	var entryA, entryB Entry
	for _, e := range store.entries {
		switch e.Content {
		case "topic alpha":
			entryA = e
		case "topic beta":
			entryB = e
		}
	}

	// Both A and B are already in candidates — expansion should NOT duplicate B.
	candidates := []recallScored{
		{entry: entryA, score: 5.0},
		{entry: entryB, score: 4.0},
	}

	result := store.graphExpand(candidates, 5)
	store.mu.RUnlock()

	// Count occurrences of B.
	countB := 0
	for _, c := range result {
		if c.entry.ID == entryB.ID {
			countB++
		}
	}
	if countB != 1 {
		t.Errorf("expected entry B exactly once, got %d", countB)
	}
}
