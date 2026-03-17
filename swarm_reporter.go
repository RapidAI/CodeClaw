package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SwarmReporter generates execution reports at the end of a swarm run.
type SwarmReporter struct{}

// NewSwarmReporter creates a SwarmReporter.
func NewSwarmReporter() *SwarmReporter { return &SwarmReporter{} }

// GenerateReport builds a SwarmReport from a completed SwarmRun.
func (r *SwarmReporter) GenerateReport(run *SwarmRun) (*SwarmReport, error) {
	if run == nil {
		return nil, fmt.Errorf("nil SwarmRun")
	}

	report := &SwarmReport{
		RunID:       run.ID,
		Mode:        run.Mode,
		Status:      run.Status,
		ProjectPath: run.ProjectPath,
		Rounds:      run.RoundHistory,
		Timeline:    run.Timeline,
		CreatedAt:   time.Now(),
	}

	// Build agent records
	var completed, failed int
	for _, a := range run.Agents {
		var dur float64
		if a.StartedAt != nil && a.CompletedAt != nil {
			dur = a.CompletedAt.Sub(*a.StartedAt).Seconds()
		}
		report.Agents = append(report.Agents, AgentRecord{
			AgentID:   a.ID,
			Role:      a.Role,
			TaskIndex: a.TaskIndex,
			Status:    a.Status,
			Duration:  dur,
		})
		switch a.Status {
		case "completed":
			completed++
		case "failed":
			failed++
		}
	}

	report.Statistics = ReportStatistics{
		TotalTasks:     len(run.Tasks),
		CompletedTasks: completed,
		FailedTasks:    failed,
		TotalRounds:    run.CurrentRound,
	}

	return report, nil
}

// WriteReportFiles writes report.md, report.json, and timeline.md to disk
// under {projectPath}/.maclaw-swarm/{runID}/.
func (r *SwarmReporter) WriteReportFiles(projectPath string, report *SwarmReport) error {
	dir := filepath.Join(projectPath, ".maclaw-swarm", report.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}

	// report.json
	jsonData, err := MarshalReport(*report)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), jsonData, 0o644); err != nil {
		return fmt.Errorf("write report.json: %w", err)
	}

	// report.md
	md := r.renderReportMD(report)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(md), 0o644); err != nil {
		return fmt.Errorf("write report.md: %w", err)
	}

	// timeline.md
	tl := r.renderTimelineMD(report)
	if err := os.WriteFile(filepath.Join(dir, "timeline.md"), []byte(tl), 0o644); err != nil {
		return fmt.Errorf("write timeline.md: %w", err)
	}

	return nil
}

// WriteDiffFile writes a diff file for a specific developer agent.
func (r *SwarmReporter) WriteDiffFile(projectPath, runID, agentID, diff string) error {
	dir := filepath.Join(projectPath, ".maclaw-swarm", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, agentID+".diff"), []byte(diff), 0o644)
}

// MarshalReport serialises a SwarmReport to JSON.
func MarshalReport(report SwarmReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// UnmarshalReport deserialises JSON into a SwarmReport. Returns an error if
// required fields (run_id, mode) are missing.
func UnmarshalReport(data []byte) (SwarmReport, error) {
	var report SwarmReport
	if err := json.Unmarshal(data, &report); err != nil {
		return SwarmReport{}, fmt.Errorf("unmarshal: %w", err)
	}
	if report.RunID == "" {
		return SwarmReport{}, fmt.Errorf("missing required field: run_id")
	}
	if report.Mode == "" {
		return SwarmReport{}, fmt.Errorf("missing required field: mode")
	}
	return report, nil
}

func (r *SwarmReporter) renderReportMD(report *SwarmReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Swarm Report: %s\n\n", report.RunID)
	fmt.Fprintf(&sb, "- Mode: %s\n", report.Mode)
	fmt.Fprintf(&sb, "- Status: %s\n", report.Status)
	fmt.Fprintf(&sb, "- Created: %s\n\n", report.CreatedAt.Format(time.RFC3339))

	// Requirement completion stats
	sb.WriteString("## Statistics\n\n")
	fmt.Fprintf(&sb, "- Total Tasks: %d\n", report.Statistics.TotalTasks)
	fmt.Fprintf(&sb, "- Completed: %d\n", report.Statistics.CompletedTasks)
	fmt.Fprintf(&sb, "- Failed: %d\n", report.Statistics.FailedTasks)
	fmt.Fprintf(&sb, "- Total Rounds: %d\n", report.Statistics.TotalRounds)

	// Code change stats
	sb.WriteString("\n## Code Changes\n\n")
	fmt.Fprintf(&sb, "- Lines Added: %d\n", report.Statistics.LinesAdded)
	fmt.Fprintf(&sb, "- Lines Modified: %d\n", report.Statistics.LinesModified)
	fmt.Fprintf(&sb, "- Lines Deleted: %d\n", report.Statistics.LinesDeleted)

	// Round details
	if len(report.Rounds) > 0 {
		sb.WriteString("\n## Rounds\n\n")
		for _, rd := range report.Rounds {
			fmt.Fprintf(&sb, "### Round %d\n\n", rd.Number)
			fmt.Fprintf(&sb, "- Reason: %s\n", rd.Reason)
			fmt.Fprintf(&sb, "- Started: %s\n", rd.StartedAt.Format(time.RFC3339))
			if rd.EndedAt != nil {
				fmt.Fprintf(&sb, "- Ended: %s\n", rd.EndedAt.Format(time.RFC3339))
			}
			fmt.Fprintf(&sb, "- Result: %s\n\n", rd.Result)
		}
	}

	// Agent workload stats
	if len(report.Agents) > 0 {
		sb.WriteString("## Agents\n\n")
		sb.WriteString("| ID | Role | Task | Status | Duration |\n")
		sb.WriteString("|---|---|---|---|---|\n")
		for _, a := range report.Agents {
			fmt.Fprintf(&sb, "| %s | %s | %d | %s | %.1fs |\n",
				a.AgentID, a.Role, a.TaskIndex, a.Status, a.Duration)
		}
	}

	// Open issues
	if len(report.OpenIssues) > 0 {
		sb.WriteString("\n## Open Issues\n\n")
		for _, issue := range report.OpenIssues {
			fmt.Fprintf(&sb, "- %s\n", issue)
		}
	}

	return sb.String()
}

func (r *SwarmReporter) renderTimelineMD(report *SwarmReport) string {
	// Sort timeline by timestamp
	events := make([]TimelineEvent, len(report.Timeline))
	copy(events, report.Timeline)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	var sb strings.Builder
	sb.WriteString("# Timeline\n\n")
	for _, e := range events {
		fmt.Fprintf(&sb, "- **%s** [%s] %s",
			e.Timestamp.Format(time.RFC3339), e.Type, e.Message)
		if e.AgentID != "" {
			fmt.Fprintf(&sb, " (agent: %s)", e.AgentID)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
