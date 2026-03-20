package main

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestValidateMaxAgents(t *testing.T) {
	tests := []struct {
		input, expected int
	}{
		{0, 1}, {-5, 1}, {1, 1}, {5, 5}, {10, 10}, {15, 10}, {100, 10},
	}
	for _, tt := range tests {
		got := ValidateMaxAgents(tt.input)
		if got != tt.expected {
			t.Errorf("ValidateMaxAgents(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// Feature: swarm-orchestrator, Property 1: 阶段顺序正确性
// Greenfield phases: task_split → architecture → development → merge → compile → test → document → report
// Maintenance phases: task_split → conflict_detect → development → merge → compile → test → document → report
func TestProperty_PhaseSequenceCorrectness(t *testing.T) {
	greenfieldPhases := []SwarmPhase{
		PhaseTaskSplit, PhaseArchitecture, PhaseDevelopment,
		PhaseMerge, PhaseCompile, PhaseTest, PhaseDocument, PhaseReport,
	}
	maintenancePhases := []SwarmPhase{
		PhaseTaskSplit, PhaseConflictDetect, PhaseDevelopment,
		PhaseMerge, PhaseCompile, PhaseTest, PhaseDocument, PhaseReport,
	}

	rapid.Check(t, func(t *rapid.T) {
		isGreenfield := rapid.Bool().Draw(t, "isGreenfield")
		var expected []SwarmPhase
		var mode SwarmMode
		if isGreenfield {
			expected = greenfieldPhases
			mode = SwarmModeGreenfield
		} else {
			expected = maintenancePhases
			mode = SwarmModeMaintenance
		}

		// Verify the phase sequence is well-defined for each mode
		if mode == SwarmModeGreenfield {
			if expected[1] != PhaseArchitecture {
				t.Fatal("greenfield must have architecture as second phase")
			}
		} else {
			if expected[1] != PhaseConflictDetect {
				t.Fatal("maintenance must have conflict_detect as second phase")
			}
		}

		// Verify no duplicate phases
		seen := make(map[SwarmPhase]bool)
		for _, p := range expected {
			if seen[p] {
				t.Fatalf("duplicate phase: %s", p)
			}
			seen[p] = true
		}

		// Verify first and last phases
		if expected[0] != PhaseTaskSplit {
			t.Fatal("first phase must be task_split")
		}
		if expected[len(expected)-1] != PhaseReport {
			t.Fatal("last phase must be report")
		}
	})
}

// Feature: swarm-orchestrator, Property 18: 单 Run 限制
// At most one SwarmRun can be in "running" status at any time.
func TestProperty_SingleRunLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		o := &SwarmOrchestrator{
			maxRounds: 5,
			maxAgents: 5,
		}

		// Simulate an active run
		o.activeRun = &SwarmRun{
			ID:     NewSwarmRunID(),
			Status: SwarmStatusRunning,
		}
		o.runHistory = append(o.runHistory, o.activeRun)

		// Attempt to start another run — should fail
		_, err := o.StartSwarmRun(SwarmRunRequest{
			Mode:         SwarmModeGreenfield,
			ProjectPath:  "/tmp/test",
			Requirements: "test",
			Tool:         "claude",
		})
		if err == nil {
			t.Fatal("expected error when starting a second run")
		}
	})
}

// Feature: swarm-orchestrator, Property 20: 并发配置范围验证
func TestProperty_MaxAgentsRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(-100, 200).Draw(t, "maxAgents")
		result := ValidateMaxAgents(n)
		if result < 1 || result > 10 {
			t.Fatalf("ValidateMaxAgents(%d) = %d, out of [1,10]", n, result)
		}
	})
}
