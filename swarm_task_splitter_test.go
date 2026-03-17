package main

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestParseManualInput_Basic(t *testing.T) {
	ts := NewTaskSplitter(MaclawLLMConfig{})
	tasks, err := ts.ParseTaskList(TaskListInput{
		Source: "manual",
		Text:   "Fix login bug\nAdd dark mode\nUpdate docs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Description != "Fix login bug" {
		t.Errorf("unexpected description: %q", tasks[0].Description)
	}
}

func TestParseManualInput_EmptyLines(t *testing.T) {
	ts := NewTaskSplitter(MaclawLLMConfig{})
	tasks, err := ts.ParseTaskList(TaskListInput{
		Source: "manual",
		Text:   "\n\nFix bug\n\n\nAdd feature\n\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestParseManualInput_Empty(t *testing.T) {
	ts := NewTaskSplitter(MaclawLLMConfig{})
	tasks, err := ts.ParseTaskList(TaskListInput{
		Source: "manual",
		Text:   "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestExtractJSON(t *testing.T) {
	input := "```json\n[{\"description\":\"test\"}]\n```"
	got := string(extractJSON([]byte(input)))
	if !strings.HasPrefix(got, "[") {
		t.Errorf("expected JSON array, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// Feature: swarm-orchestrator, Property 3: 手动输入解析完整性
// For any text with N non-empty lines, ParseTaskList should produce exactly
// N SubTasks, each with Description matching the corresponding line.
func TestProperty_ManualInputParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random non-empty lines
		n := rapid.IntRange(1, 20).Draw(t, "lineCount")
		lines := make([]string, n)
		for i := 0; i < n; i++ {
			// Generate a non-empty, non-whitespace-only string
			line := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(t, "line")
			lines[i] = line
		}
		text := strings.Join(lines, "\n")

		ts := NewTaskSplitter(MaclawLLMConfig{})
		tasks, err := ts.ParseTaskList(TaskListInput{Source: "manual", Text: text})
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != n {
			t.Fatalf("expected %d tasks, got %d", n, len(tasks))
		}

		for i, task := range tasks {
			if task.Description != lines[i] {
				t.Fatalf("task %d: expected %q, got %q", i, lines[i], task.Description)
			}
			if task.Index != i {
				t.Fatalf("task %d: expected Index=%d, got %d", i, i, task.Index)
			}
		}
	})
}

// Feature: swarm-orchestrator, Property 4: Maintenance 模式创建正确性
// This property is tested at the orchestrator level. Here we verify that
// ParseTaskList with any valid source produces tasks with sequential indices.
func TestProperty_TaskIndicesSequential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 15).Draw(t, "lineCount")
		lines := make([]string, n)
		for i := 0; i < n; i++ {
			lines[i] = rapid.StringMatching(`[a-zA-Z]{1,30}`).Draw(t, "line")
		}

		ts := NewTaskSplitter(MaclawLLMConfig{})
		tasks, err := ts.ParseTaskList(TaskListInput{Source: "manual", Text: strings.Join(lines, "\n")})
		if err != nil {
			t.Fatal(err)
		}

		for i, task := range tasks {
			if task.Index != i {
				t.Fatalf("expected sequential index %d, got %d", i, task.Index)
			}
		}
	})
}
