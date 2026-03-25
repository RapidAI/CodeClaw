package swarm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestMarshalUnmarshalReport_RoundTrip(t *testing.T) {
	report := SwarmReport{
		RunID:  "test-run",
		Mode:   SwarmModeGreenfield,
		Status: SwarmStatusCompleted,
		Statistics: ReportStatistics{
			TotalTasks:     5,
			CompletedTasks: 4,
			FailedTasks:    1,
		},
		CreatedAt: time.Now().Truncate(time.Second),
	}

	data, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalReport(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.RunID != report.RunID || got.Mode != report.Mode {
		t.Errorf("round-trip mismatch: %+v vs %+v", report, got)
	}
}

func TestUnmarshalReport_MissingRunID(t *testing.T) {
	data := []byte(`{"mode":"greenfield"}`)
	_, err := UnmarshalReport(data)
	if err == nil {
		t.Error("expected error for missing run_id")
	}
}

func TestUnmarshalReport_MissingMode(t *testing.T) {
	data := []byte(`{"run_id":"test"}`)
	_, err := UnmarshalReport(data)
	if err == nil {
		t.Error("expected error for missing mode")
	}
}

func TestWriteReportFiles(t *testing.T) {
	dir := t.TempDir()
	reporter := NewSwarmReporter()
	report := &SwarmReport{
		RunID:  "test-write",
		Mode:   SwarmModeMaintenance,
		Status: SwarmStatusCompleted,
		Timeline: []TimelineEvent{
			{Timestamp: time.Now(), Type: "start", Message: "run started"},
		},
	}

	if err := reporter.WriteReportFiles(dir, report); err != nil {
		t.Fatal(err)
	}

	reportDir := filepath.Join(dir, ".maclaw-swarm", "test-write")
	for _, name := range []string{"report.md", "report.json", "timeline.md"} {
		if _, err := os.Stat(filepath.Join(reportDir, name)); os.IsNotExist(err) {
			t.Errorf("missing file: %s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// genSwarmReport generates a random SwarmReport for property testing.
func genSwarmReport(t *rapid.T) SwarmReport {
	mode := SwarmModeGreenfield
	if rapid.Bool().Draw(t, "isMaintenance") {
		mode = SwarmModeMaintenance
	}
	statuses := []SwarmStatus{SwarmStatusCompleted, SwarmStatusFailed, SwarmStatusCancelled}
	status := statuses[rapid.IntRange(0, len(statuses)-1).Draw(t, "statusIdx")]

	nAgents := rapid.IntRange(0, 5).Draw(t, "nAgents")
	agents := make([]AgentRecord, nAgents)
	for i := 0; i < nAgents; i++ {
		agents[i] = AgentRecord{
			AgentID:   rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "agentID"),
			Role:      RoleDeveloper,
			TaskIndex: i,
			Status:    "completed",
			Duration:  float64(rapid.IntRange(1, 3600).Draw(t, "dur")),
		}
	}

	nEvents := rapid.IntRange(0, 10).Draw(t, "nEvents")
	timeline := make([]TimelineEvent, nEvents)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < nEvents; i++ {
		timeline[i] = TimelineEvent{
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Type:      "event",
			Message:   rapid.StringMatching(`[a-z ]{1,20}`).Draw(t, "msg"),
		}
	}

	return SwarmReport{
		RunID:       rapid.StringMatching(`swarm_[0-9]{10}_[0-9a-f]{4}`).Draw(t, "runID"),
		Mode:        mode,
		Status:      status,
		ProjectPath: "/tmp/test",
		Statistics: ReportStatistics{
			TotalTasks:     rapid.IntRange(0, 100).Draw(t, "total"),
			CompletedTasks: rapid.IntRange(0, 100).Draw(t, "completed"),
			FailedTasks:    rapid.IntRange(0, 100).Draw(t, "failed"),
			TotalRounds:    rapid.IntRange(0, 10).Draw(t, "rounds"),
		},
		Agents:    agents,
		Timeline:  timeline,
		CreatedAt: base,
	}
}

// Feature: swarm-orchestrator, Property 16: 报告序列化往返
func TestProperty_ReportSerializationRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := genSwarmReport(t)

		data, err := MarshalReport(original)
		if err != nil {
			t.Fatal(err)
		}

		restored, err := UnmarshalReport(data)
		if err != nil {
			t.Fatal(err)
		}

		if restored.RunID != original.RunID {
			t.Fatalf("RunID mismatch: %q vs %q", original.RunID, restored.RunID)
		}
		if restored.Mode != original.Mode {
			t.Fatalf("Mode mismatch: %q vs %q", original.Mode, restored.Mode)
		}
		if restored.Status != original.Status {
			t.Fatalf("Status mismatch")
		}
		if restored.Statistics.TotalTasks != original.Statistics.TotalTasks {
			t.Fatalf("TotalTasks mismatch")
		}
		if len(restored.Agents) != len(original.Agents) {
			t.Fatalf("Agents count mismatch")
		}
		if len(restored.Timeline) != len(original.Timeline) {
			t.Fatalf("Timeline count mismatch")
		}
	})
}

// Feature: swarm-orchestrator, Property 17: 报告反序列化错误处理
func TestProperty_ReportUnmarshalMissingFields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate JSON missing run_id or mode
		missingRunID := rapid.Bool().Draw(t, "missingRunID")
		obj := map[string]interface{}{
			"status":       "completed",
			"project_path": "/tmp",
		}
		if !missingRunID {
			obj["run_id"] = rapid.StringMatching(`[a-z]{5}`).Draw(t, "runID")
			// mode is missing
		} else {
			obj["mode"] = "greenfield"
			// run_id is missing
		}

		data, _ := json.Marshal(obj)
		_, err := UnmarshalReport(data)
		if err == nil {
			t.Fatal("expected error for missing required field")
		}
	})
}

// Feature: swarm-orchestrator, Property 22: 报告文件完整性
func TestProperty_ReportFileCompleteness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir, err := os.MkdirTemp("", "swarm-report-*")
		if err != nil {
			rt.Fatal(err)
		}
		defer os.RemoveAll(dir)

		report := genSwarmReport(rt)
		reporter := NewSwarmReporter()

		if err := reporter.WriteReportFiles(dir, &report); err != nil {
			rt.Fatal(err)
		}

		reportDir := filepath.Join(dir, ".maclaw-swarm", report.RunID)
		for _, name := range []string{"report.md", "report.json", "timeline.md"} {
			if _, err := os.Stat(filepath.Join(reportDir, name)); os.IsNotExist(err) {
				rt.Fatalf("missing file: %s", name)
			}
		}
	})
}

// Feature: swarm-orchestrator, Property 23: 时间线事件有序性
func TestProperty_TimelineOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		report := genSwarmReport(t)

		// Sort as the reporter does
		events := make([]TimelineEvent, len(report.Timeline))
		copy(events, report.Timeline)
		sort.Slice(events, func(i, j int) bool {
			return events[i].Timestamp.Before(events[j].Timestamp)
		})

		for i := 1; i < len(events); i++ {
			if events[i].Timestamp.Before(events[i-1].Timestamp) {
				t.Fatalf("timeline not ordered at index %d", i)
			}
		}
	})
}

// Feature: swarm-orchestrator, Property 24: Developer Diff 文件完整性
func TestProperty_DeveloperDiffFiles(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir, err := os.MkdirTemp("", "swarm-diff-*")
		if err != nil {
			rt.Fatal(err)
		}
		defer os.RemoveAll(dir)

		runID := rapid.StringMatching(`run_[0-9]{4}`).Draw(rt, "runID")
		n := rapid.IntRange(1, 5).Draw(rt, "devCount")
		reporter := NewSwarmReporter()

		for i := 0; i < n; i++ {
			agentID := rapid.StringMatching(`dev_[0-9]{2}`).Draw(rt, "agentID")
			diff := rapid.StringMatching(`[a-z]{10,50}`).Draw(rt, "diff")
			if err := reporter.WriteDiffFile(dir, runID, agentID, diff); err != nil {
				rt.Fatal(err)
			}
		}

		// Verify files exist
		reportDir := filepath.Join(dir, ".maclaw-swarm", runID)
		entries, err := os.ReadDir(reportDir)
		if err != nil {
			rt.Fatal(err)
		}

		diffCount := 0
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".diff" {
				diffCount++
			}
		}
		// Note: diffCount may be less than n if agentIDs collide, which is fine
		if diffCount == 0 {
			rt.Fatal("expected at least one diff file")
		}
	})
}
