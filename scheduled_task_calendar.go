package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// isRecurringTask returns true if the task is not a one-time task.
// A one-time task has StartDate == EndDate (both set to the same date).
func isRecurringTask(t *ScheduledTask) bool {
	if t.StartDate == "" && t.EndDate == "" {
		// No date range → recurring (every day / every week etc.)
		return true
	}
	if t.StartDate != "" && t.EndDate != "" && t.StartDate == t.EndDate {
		// Same start and end date → one-time
		return false
	}
	return true
}

// SyncTaskToSystemCalendar creates an ICS file for a recurring scheduled task
// and opens it with the default calendar application. One-time tasks are skipped.
func SyncTaskToSystemCalendar(task *ScheduledTask) error {
	if !isRecurringTask(task) {
		return nil // 一次性任务不同步到日历
	}

	ics := buildICSEvent(task)
	if ics == "" {
		return fmt.Errorf("failed to build ICS event")
	}

	// Write to temp file
	tmpDir := os.TempDir()
	icsPath := filepath.Join(tmpDir, fmt.Sprintf("maclaw_task_%s.ics", task.ID))
	if err := os.WriteFile(icsPath, []byte(ics), 0644); err != nil {
		return fmt.Errorf("write ICS file: %w", err)
	}

	// Open with default calendar app
	return openFile(icsPath)
}

// buildICSEvent generates an ICS (iCalendar) event string for a recurring task.
func buildICSEvent(t *ScheduledTask) string {
	now := time.Now()
	uid := fmt.Sprintf("maclaw-task-%s@maclaw.local", t.ID)

	// Determine DTSTART
	var dtStart time.Time
	if t.StartDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", t.StartDate, time.Local); err == nil {
			dtStart = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), t.Hour, t.Minute, 0, 0, time.Local)
		}
	}
	if dtStart.IsZero() {
		// No start date: start from today or next occurrence
		dtStart = time.Date(now.Year(), now.Month(), now.Day(), t.Hour, t.Minute, 0, 0, time.Local)
		if dtStart.Before(now) {
			dtStart = dtStart.AddDate(0, 0, 1)
		}
	}

	// Build RRULE
	rrule := buildRRule(t)

	// Build UNTIL for RRULE if end date is set
	var untilStr string
	if t.EndDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", t.EndDate, time.Local); err == nil {
			until := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, time.UTC)
			untilStr = fmt.Sprintf(";UNTIL=%s", until.Format("20060102T150405Z"))
		}
	}

	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//MaClaw//ScheduledTask//CN\r\n")
	b.WriteString("BEGIN:VEVENT\r\n")
	b.WriteString(fmt.Sprintf("UID:%s\r\n", uid))
	b.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", now.UTC().Format("20060102T150405Z")))
	b.WriteString(fmt.Sprintf("DTSTART:%s\r\n", dtStart.UTC().Format("20060102T150405Z")))
	b.WriteString(fmt.Sprintf("DTEND:%s\r\n", dtStart.Add(30*time.Minute).UTC().Format("20060102T150405Z")))
	b.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", escapeICS(t.Name)))
	b.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", escapeICS(t.Action)))
	if rrule != "" {
		b.WriteString(fmt.Sprintf("RRULE:%s%s\r\n", rrule, untilStr))
	}
	// Alarm: remind at event time
	b.WriteString("BEGIN:VALARM\r\n")
	b.WriteString("TRIGGER:PT0M\r\n")
	b.WriteString("ACTION:DISPLAY\r\n")
	b.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", escapeICS(t.Name)))
	b.WriteString("END:VALARM\r\n")
	b.WriteString("END:VEVENT\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// buildRRule generates the RRULE string for a scheduled task.
func buildRRule(t *ScheduledTask) string {
	if t.DayOfMonth > 0 {
		// Monthly on a specific day
		return fmt.Sprintf("FREQ=MONTHLY;BYMONTHDAY=%d", t.DayOfMonth)
	}
	if t.DayOfWeek >= 0 && t.DayOfWeek <= 6 {
		// Weekly on a specific day
		days := []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
		return fmt.Sprintf("FREQ=WEEKLY;BYDAY=%s", days[t.DayOfWeek])
	}
	// Every day
	return "FREQ=DAILY"
}

// escapeICS escapes special characters for ICS format.
func escapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// openFile opens a file with the default system application.
func openFile(path string) error {
	switch goruntime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default: // linux, freebsd, etc.
		return exec.Command("xdg-open", path).Start()
	}
}
