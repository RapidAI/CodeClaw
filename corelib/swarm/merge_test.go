package swarm

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestTopologicalSort_AlreadySorted(t *testing.T) {
	branches := []BranchInfo{
		{Name: "a", Order: 0},
		{Name: "b", Order: 1},
		{Name: "c", Order: 2},
	}
	sorted := TopologicalSort(branches)
	for i, b := range sorted {
		if b.Order != i {
			t.Errorf("expected order %d at index %d, got %d", i, i, b.Order)
		}
	}
}

func TestTopologicalSort_Reversed(t *testing.T) {
	branches := []BranchInfo{
		{Name: "c", Order: 2},
		{Name: "a", Order: 0},
		{Name: "b", Order: 1},
	}
	sorted := TopologicalSort(branches)
	if sorted[0].Name != "a" || sorted[1].Name != "b" || sorted[2].Name != "c" {
		t.Errorf("unexpected order: %v", sorted)
	}
}

func TestTopologicalSort_Empty(t *testing.T) {
	sorted := TopologicalSort(nil)
	if len(sorted) != 0 {
		t.Errorf("expected empty, got %d", len(sorted))
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// Feature: swarm-orchestrator, Property 13: 拓扑序合并
// For any set of branches with dependency ordering, TopologicalSort must
// produce a sequence where Order values are non-decreasing.
func TestProperty_TopologicalSortOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 20).Draw(t, "branchCount")
		branches := make([]BranchInfo, n)
		for i := 0; i < n; i++ {
			branches[i] = BranchInfo{
				Name:  rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "name"),
				Order: rapid.IntRange(0, 100).Draw(t, "order"),
			}
		}

		sorted := TopologicalSort(branches)

		if len(sorted) != n {
			t.Fatalf("expected %d branches, got %d", n, len(sorted))
		}

		// Verify non-decreasing order
		for i := 1; i < len(sorted); i++ {
			if sorted[i].Order < sorted[i-1].Order {
				t.Fatalf("order not non-decreasing at index %d: %d < %d",
					i, sorted[i].Order, sorted[i-1].Order)
			}
		}
	})
}
