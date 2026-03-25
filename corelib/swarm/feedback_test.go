package swarm

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestFeedbackLoop_ShouldContinue(t *testing.T) {
	fl := NewFeedbackLoop(nil, 3)
	if !fl.ShouldContinue() {
		t.Error("should continue at round 0")
	}
	fl.NextRound("test")
	fl.NextRound("test")
	fl.NextRound("test")
	if fl.ShouldContinue() {
		t.Error("should not continue at max rounds")
	}
}

func TestFeedbackLoop_DefaultMaxRounds(t *testing.T) {
	fl := NewFeedbackLoop(nil, 0)
	if fl.MaxRounds() != 5 {
		t.Errorf("expected default 5, got %d", fl.MaxRounds())
	}
}

func TestFeedbackLoop_ClassifyFailures_NilCaller(t *testing.T) {
	fl := NewFeedbackLoop(nil, 3)
	_, err := fl.ClassifyFailures([]TestFailure{{TestName: "TestFoo", ErrorOutput: "fail"}})
	if err == nil {
		t.Fatal("expected error when caller is nil")
	}
	if err.Error() != "LLM caller not configured, cannot classify failures" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestDetermineStrategy(t *testing.T) {
	tests := []struct {
		ft       FailureType
		expected string
	}{
		{FailureTypeBug, "maintenance_round"},
		{FailureTypeFeatureGap, "mini_greenfield"},
		{FailureTypeRequirementDeviation, "pause_for_user"},
	}
	for _, tt := range tests {
		got := DetermineStrategy(ClassifiedFailure{Type: tt.ft})
		if got != tt.expected {
			t.Errorf("DetermineStrategy(%s) = %q, want %q", tt.ft, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// Feature: swarm-orchestrator, Property 15: 轮次计数器单调递增与终止
// Each NextRound call increments round by exactly 1, and ShouldContinue
// returns false when round reaches maxRounds.
func TestProperty_RoundCounterMonotonic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxRounds := rapid.IntRange(1, 20).Draw(t, "maxRounds")
		fl := NewFeedbackLoop(nil, maxRounds)

		calls := rapid.IntRange(0, maxRounds+5).Draw(t, "calls")
		for i := 0; i < calls; i++ {
			prev := fl.Round()
			fl.NextRound("test")
			if fl.Round() != prev+1 {
				t.Fatalf("round did not increment by 1: was %d, now %d", prev, fl.Round())
			}
		}

		if fl.Round() >= maxRounds && fl.ShouldContinue() {
			t.Fatalf("ShouldContinue=true at round %d with max %d", fl.Round(), maxRounds)
		}
		if fl.Round() < maxRounds && !fl.ShouldContinue() {
			t.Fatalf("ShouldContinue=false at round %d with max %d", fl.Round(), maxRounds)
		}
	})
}

// Feature: swarm-orchestrator, Property 14: 失败类型路由正确性
// Bug → maintenance_round, FeatureGap → mini_greenfield,
// RequirementDeviation → pause_for_user.
func TestProperty_FailureTypeRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		types := []FailureType{FailureTypeBug, FailureTypeFeatureGap, FailureTypeRequirementDeviation}
		idx := rapid.IntRange(0, len(types)-1).Draw(t, "typeIdx")
		ft := types[idx]

		strategy := DetermineStrategy(ClassifiedFailure{Type: ft})

		expected := map[FailureType]string{
			FailureTypeBug:                  "maintenance_round",
			FailureTypeFeatureGap:           "mini_greenfield",
			FailureTypeRequirementDeviation: "pause_for_user",
		}

		if strategy != expected[ft] {
			t.Fatalf("DetermineStrategy(%s) = %q, want %q", ft, strategy, expected[ft])
		}
	})
}
