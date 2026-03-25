package swarm

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
			got := string(ExtractJSONObject([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateForPrompt(t *testing.T) {
	short := "hello"
	if got := TruncateForPrompt(short, 100); got != short {
		t.Errorf("short string should not be truncated, got %q", got)
	}

	long := "这是一段很长的中文文本用于测试截断功能"
	got := TruncateForPrompt(long, 5)
	runes := []rune(got)
	// Should be 5 runes + truncation suffix
	if len(runes) < 5 {
		t.Errorf("truncated too aggressively: %q", got)
	}
}

func TestTaskVerifier_EmptyOutput(t *testing.T) {
	v := NewTaskVerifier(nil)
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

func TestTaskVerifier_NilCaller(t *testing.T) {
	v := NewTaskVerifier(nil)
	_, err := v.Verify("implement login", "some output here")
	if err == nil {
		t.Error("expected error when caller is nil and output is non-empty")
	}
}
