package main

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: swarm-orchestrator, Property 25: Run ID 唯一性
// For any two different SwarmRun, their IDs must be different.
func TestProperty_RunIDUniqueness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 50).Draw(t, "count")
		seen := make(map[string]bool, n)
		for i := 0; i < n; i++ {
			id := NewSwarmRunID()
			if seen[id] {
				t.Fatalf("duplicate run ID: %s", id)
			}
			seen[id] = true
		}
	})
}
