package main

import (
	"testing"
)

func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain JSON",
			input: `{"pass": true, "score": 85, "reason": "done", "missing": ""}`,
			want:  `{"pass": true, "score": 85, "reason": "done", "missing": ""}`,
		},
		{
			name:  "markdown fenced",
			input: "```json\n{\"pass\": false, \"score\": 20}\n```",
			want:  `{"pass": false, "score": 20}`,
		},
		{
			name:  "with surrounding text",
			input: "Here is the result:\n{\"pass\": true}\nEnd.",
			want:  `{"pass": true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(extractJSONObject([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateForPrompt(t *testing.T) {
	short := "hello"
	if got := truncateForPrompt(short, 100); got != short {
		t.Errorf("short string should not be truncated, got %q", got)
	}

	long := "这是一段很长的中文文本用于测试截断功能"
	got := truncateForPrompt(long, 5)
	runes := []rune(got)
	// Should be 5 runes + truncation suffix
	if len(runes) < 5 {
		t.Errorf("truncated too aggressively: %q", got)
	}
}

func TestTaskVerifier_EmptyOutput(t *testing.T) {
	v := NewTaskVerifier(MaclawLLMConfig{})
	verdict, err := v.Verify("implement login", "")
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Pass {
		t.Error("empty output should not pass")
	}
	if verdict.Score != 0 {
		t.Errorf("score = %d, want 0", verdict.Score)
	}
}

func TestSelectToolForTask_FixedTool(t *testing.T) {
	o := &SwarmOrchestrator{
		toolSelector: NewToolSelector(),
	}
	run := &SwarmRun{Tool: "gemini"}
	task := SubTask{Description: "implement login with React"}

	tool, reason := o.selectToolForTask(run, task)
	if tool != "gemini" {
		t.Errorf("tool = %q, want gemini (user specified)", tool)
	}
	if reason != "用户指定工具" {
		t.Errorf("reason = %q, want 用户指定工具", reason)
	}
}

func TestSelectToolForTask_AutoSelect(t *testing.T) {
	o := &SwarmOrchestrator{
		toolSelector: NewToolSelector(),
	}
	run := &SwarmRun{Tool: "", TechStack: "go"}
	task := SubTask{Description: "refactor the authentication module"}

	tool, _ := o.selectToolForTask(run, task)
	if tool == "" {
		t.Error("tool should not be empty")
	}
}

func TestVerifyAgentOutput_NilVerifier(t *testing.T) {
	o := &SwarmOrchestrator{}
	run := &SwarmRun{}
	agent := &SwarmAgent{Output: "some output"}
	task := SubTask{Description: "do something"}

	verdict := o.verifyAgentOutput(run, agent, task)
	if verdict != nil {
		t.Error("nil verifier should return nil verdict")
	}
}

func TestVerifyAgentOutput_EmptyOutput(t *testing.T) {
	o := &SwarmOrchestrator{
		taskVerifier: NewTaskVerifier(MaclawLLMConfig{}),
	}
	run := &SwarmRun{}
	agent := &SwarmAgent{Output: ""}
	task := SubTask{Description: "do something"}

	verdict := o.verifyAgentOutput(run, agent, task)
	if verdict != nil {
		t.Error("empty output should return nil (skipped)")
	}
}
