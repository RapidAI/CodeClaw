package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type EventExtractor interface {
	Consume(session *RemoteSession, lines []string) []ImportantEvent
}

type ClaudeEventExtractor struct{}

func NewClaudeEventExtractor() *ClaudeEventExtractor {
	return &ClaudeEventExtractor{}
}

func (e *ClaudeEventExtractor) Consume(session *RemoteSession, lines []string) []ImportantEvent {
	events := make([]ImportantEvent, 0)
	for _, line := range lines {
		if evt := e.detectFileRead(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectFileChanged(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectCommandStarted(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectInputRequired(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectError(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
	}
	return events
}

func (e *ClaudeEventExtractor) detectFileRead(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	if !containsAny(lower, []string{"reading ", "read file", "inspecting ", "opened "}) {
		return nil
	}

	file := extractFilePath(line)
	return newEvent(session, "file.read", "info", "Inspected file", line, file, "")
}

func (e *ClaudeEventExtractor) detectFileChanged(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	if !containsAny(lower, []string{"editing ", "modified ", "updated ", "patched ", "created ", "wrote ", "rewrote "}) {
		return nil
	}

	file := extractFilePath(line)
	return newEvent(session, "file.change", "info", "Changed file", line, file, "")
}

func (e *ClaudeEventExtractor) detectCommandStarted(session *RemoteSession, line string) *ImportantEvent {
	command, ok := extractCommand(line)
	if !ok {
		return nil
	}
	return newEvent(session, "command.started", "info", "Running command", line, "", command)
}

func (e *ClaudeEventExtractor) detectInputRequired(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	keywords := []string{
		"need your input",
		"waiting for input",
		"please confirm",
		"continue?",
		"choose an option",
		"approve",
		"yes/no",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "input.required", "warn", "Waiting for your input", line, "", "")
		}
	}
	return nil
}

func (e *ClaudeEventExtractor) detectError(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	if containsAny(lower, []string{"0 errors", "without errors", "no error", "error count: 0"}) {
		return nil
	}

	keywords := []string{"error:", "failed", "panic:", "traceback", "exit status", "exception"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "session.error", "error", "Error detected", line, "", "")
		}
	}
	return nil
}

func newEvent(session *RemoteSession, typ, severity, title, summary, relatedFile, command string) *ImportantEvent {
	return &ImportantEvent{
		EventID:     fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID:   session.ID,
		MachineID:   session.Summary.MachineID,
		Type:        typ,
		Severity:    severity,
		Title:       title,
		Summary:     summary,
		Count:       1,
		RelatedFile: relatedFile,
		Command:     command,
		CreatedAt:   time.Now().Unix(),
	}
}

func containsAny(value string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(value, kw) {
			return true
		}
	}
	return false
}

func extractCommand(line string) (string, bool) {
	lower := strings.ToLower(line)

	for _, prefix := range []string{"running ", "executing ", "command: "} {
		if idx := strings.Index(lower, prefix); idx >= 0 {
			command := strings.TrimSpace(line[idx+len(prefix):])
			if command != "" {
				return trimCommandMarker(command), true
			}
		}
	}

	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{"$ ", "> ", "# "} {
		if strings.HasPrefix(trimmed, marker) {
			command := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
			if command != "" {
				return command, true
			}
		}
	}

	knownCommands := []string{"go test", "go build", "pytest", "npm test", "pnpm test", "cargo test", "cargo build", "python ", "node "}
	for _, command := range knownCommands {
		if strings.HasPrefix(lower, command) {
			return trimmed, true
		}
	}

	return "", false
}

func trimCommandMarker(command string) string {
	command = strings.TrimSpace(command)
	command = strings.TrimPrefix(command, "$ ")
	command = strings.TrimPrefix(command, "> ")
	command = strings.TrimPrefix(command, "# ")
	return strings.TrimSpace(command)
}

func extractFilePath(line string) string {
	for _, token := range strings.Fields(line) {
		candidate := strings.Trim(token, "\"'`()[]{}")
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, "/") || strings.Contains(candidate, "\\") {
			return filepath.Clean(candidate)
		}
		if hasLikelyFileExtension(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

func hasLikelyFileExtension(value string) bool {
	ext := strings.ToLower(filepath.Ext(value))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".md", ".yaml", ".yml", ".py", ".java", ".rs", ".sh", ".css", ".html":
		return true
	default:
		return false
	}
}
