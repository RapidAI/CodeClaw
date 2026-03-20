package remote

import (
	"fmt"
	"strings"
	"time"
)

// PreviewBuffer accumulates output lines and produces preview deltas.
type PreviewBuffer interface {
	Append(sessionID string, lines []string) *SessionPreviewDelta
}

// EventExtractor parses raw output lines and extracts ImportantEvent items.
type EventExtractor interface {
	Consume(sessionID string, summary SessionSummary, lines []string) []ImportantEvent
}

// SummaryReducer updates a SessionSummary based on events and raw lines.
type SummaryReducer interface {
	Apply(current SessionSummary, events []ImportantEvent, lines []string) SessionSummary
}

// OutputPipeline processes raw PTY/SDK output chunks into structured
// summaries, preview deltas, and important events.
type OutputPipeline struct {
	buffer            PreviewBuffer
	extract           EventExtractor
	reducer           SummaryReducer
	recentEventTimes  map[string]time.Time
	dedupeWindow      time.Duration
	recentFileBursts  map[string]fileBurst
	recentCommandRuns map[string]time.Time
	burstWindow       time.Duration
}

type fileBurst struct {
	eventType string
	files     []string
	lastSeen  time.Time
}

// NewOutputPipeline creates a pipeline with the given components.
func NewOutputPipeline(buf PreviewBuffer, ext EventExtractor, red SummaryReducer) *OutputPipeline {
	return &OutputPipeline{
		buffer:            buf,
		extract:           ext,
		reducer:           red,
		recentEventTimes:  map[string]time.Time{},
		dedupeWindow:      2 * time.Second,
		recentFileBursts:  map[string]fileBurst{},
		recentCommandRuns: map[string]time.Time{},
		burstWindow:       4 * time.Second,
	}
}

// Consume processes a raw output chunk and returns structured results.
func (p *OutputPipeline) Consume(sessionID string, summary SessionSummary, chunk []byte) OutputResult {
	lines := NormalizeChunkLines(chunk)
	if len(lines) == 0 {
		return OutputResult{}
	}

	events := p.coalesceAcrossBursts(sessionID, p.coalesceEvents(p.filterDuplicateEvents(p.extract.Consume(sessionID, summary, lines))))
	newSummary := p.reducer.Apply(summary, events, lines)
	previewDelta := p.buffer.Append(sessionID, lines)

	return OutputResult{
		Summary:      &newSummary,
		PreviewDelta: previewDelta,
		Events:       events,
	}
}

func (p *OutputPipeline) filterDuplicateEvents(events []ImportantEvent) []ImportantEvent {
	if len(events) == 0 {
		return events
	}

	now := time.Now()
	filtered := make([]ImportantEvent, 0, len(events))
	for _, event := range events {
		key := BuildEventDedupeKey(event)
		lastSeen, exists := p.recentEventTimes[key]
		if exists && now.Sub(lastSeen) < p.dedupeWindow {
			continue
		}
		p.recentEventTimes[key] = now
		filtered = append(filtered, event)
	}

	for key, seenAt := range p.recentEventTimes {
		if now.Sub(seenAt) > 5*p.dedupeWindow {
			delete(p.recentEventTimes, key)
		}
	}

	return filtered
}

func (p *OutputPipeline) coalesceEvents(events []ImportantEvent) []ImportantEvent {
	if len(events) < 2 {
		return events
	}

	merged := make([]ImportantEvent, 0, len(events))
	for i := 0; i < len(events); {
		event := events[i]
		if event.Type != "file.change" && event.Type != "file.read" {
			merged = append(merged, event)
			i++
			continue
		}

		j := i + 1
		files := []string{}
		seen := map[string]struct{}{}
		if event.RelatedFile != "" {
			files = append(files, event.RelatedFile)
			seen[event.RelatedFile] = struct{}{}
		}

		for j < len(events) && events[j].Type == event.Type {
			if file := events[j].RelatedFile; file != "" {
				if _, ok := seen[file]; !ok {
					files = append(files, file)
					seen[file] = struct{}{}
				}
			}
			j++
		}

		if len(files) <= 1 {
			event.Count = 1
			merged = append(merged, event)
			i++
			continue
		}

		event.Title = BuildMergedEventTitle(event.Type, len(files))
		event.Summary = BuildMergedEventSummary(event.Type, files)
		event.Count = len(files)
		event.Grouped = true
		event.RelatedFile = files[len(files)-1]
		merged = append(merged, event)
		i = j
	}

	return merged
}

func (p *OutputPipeline) coalesceAcrossBursts(sessionID string, events []ImportantEvent) []ImportantEvent {
	if len(events) == 0 || sessionID == "" {
		return events
	}

	now := time.Now()
	merged := make([]ImportantEvent, 0, len(events))
	for _, event := range events {
		if event.Type == "command.started" {
			key := sessionID + "|" + event.Type + "|" + event.Command
			lastSeen, ok := p.recentCommandRuns[key]
			if ok && now.Sub(lastSeen) <= p.burstWindow {
				continue
			}
			p.recentCommandRuns[key] = now
			merged = append(merged, event)
			continue
		}
		if event.Type != "file.change" && event.Type != "file.read" {
			merged = append(merged, event)
			continue
		}

		key := sessionID + "|" + event.Type
		burst, ok := p.recentFileBursts[key]
		if !ok || now.Sub(burst.lastSeen) > p.burstWindow {
			files := CollectEventFiles(event)
			p.recentFileBursts[key] = fileBurst{
				eventType: event.Type,
				files:     files,
				lastSeen:  now,
			}
			merged = append(merged, event)
			continue
		}

		files, changed := MergeBurstFiles(burst.files, CollectEventFiles(event))
		burst.files = files
		burst.lastSeen = now
		p.recentFileBursts[key] = burst
		if !changed {
			continue
		}
		if len(files) > 1 {
			event.Title = BuildMergedEventTitle(event.Type, len(files))
			event.Summary = BuildMergedEventSummary(event.Type, files)
			event.Count = len(files)
			event.Grouped = true
			event.RelatedFile = files[len(files)-1]
		} else if event.Count == 0 {
			event.Count = 1
		}
		merged = append(merged, event)
	}

	for key, burst := range p.recentFileBursts {
		if now.Sub(burst.lastSeen) > 2*p.burstWindow {
			delete(p.recentFileBursts, key)
		}
	}
	for key, seenAt := range p.recentCommandRuns {
		if now.Sub(seenAt) > 2*p.burstWindow {
			delete(p.recentCommandRuns, key)
		}
	}

	return merged
}

// BuildEventDedupeKey builds a deduplication key for an event.
func BuildEventDedupeKey(event ImportantEvent) string {
	var b strings.Builder
	b.Grow(len(event.Type) + len(event.RelatedFile) + len(event.Command) + len(event.Summary) + 3)
	b.WriteString(event.Type)
	b.WriteByte('|')
	b.WriteString(event.RelatedFile)
	b.WriteByte('|')
	b.WriteString(event.Command)
	b.WriteByte('|')
	b.WriteString(event.Summary)
	return b.String()
}

// BuildMergedEventTitle builds a title for merged file events.
func BuildMergedEventTitle(eventType string, count int) string {
	switch eventType {
	case "file.read":
		return fmt.Sprintf("Inspected %d files", count)
	case "file.change":
		return fmt.Sprintf("Changed %d files", count)
	default:
		return fmt.Sprintf("%d events", count)
	}
}

// BuildMergedEventSummary builds a summary for merged file events.
func BuildMergedEventSummary(eventType string, files []string) string {
	verb := "Updated"
	if eventType == "file.read" {
		verb = "Inspected"
	}

	preview := files
	if len(preview) > 3 {
		preview = preview[:3]
	}

	summary := fmt.Sprintf("%s %d files", verb, len(files))
	if len(preview) > 0 {
		summary += ": " + strings.Join(preview, ", ")
		if len(files) > len(preview) {
			summary += ", ..."
		}
	}
	return summary
}

// CollectEventFiles extracts file paths from an event.
func CollectEventFiles(event ImportantEvent) []string {
	files := []string{}
	if event.RelatedFile != "" {
		files = append(files, event.RelatedFile)
	}
	if idx := strings.Index(event.Summary, ": "); idx >= 0 {
		for _, part := range strings.Split(event.Summary[idx+2:], ",") {
			file := strings.TrimSpace(strings.TrimSuffix(part, "..."))
			if file == "" {
				continue
			}
			files, _ = MergeBurstFiles(files, []string{file})
		}
	}
	return files
}

// MergeBurstFiles merges incoming files into existing, returning the
// combined list and whether any new files were added.
func MergeBurstFiles(existing []string, incoming []string) ([]string, bool) {
	if len(incoming) == 0 {
		return existing, false
	}

	seen := make(map[string]struct{}, len(existing))
	out := append([]string(nil), existing...)
	for _, file := range existing {
		if file == "" {
			continue
		}
		seen[file] = struct{}{}
	}

	changed := false
	for _, file := range incoming {
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		out = append(out, file)
		changed = true
	}
	return out, changed
}
