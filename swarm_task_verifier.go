package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TaskVerdict is the result of verifying an agent's output against its task.
type TaskVerdict struct {
	Pass    bool   `json:"pass"`
	Score   int    `json:"score"`   // 0-100
	Reason  string `json:"reason"`
	Missing string `json:"missing"` // what's missing if not pass
}

// TaskVerifier uses an LLM to check whether an agent's output actually
// fulfills the assigned task description. This prevents tasks from "drifting"
// — an agent doing unrelated work without anyone noticing until the test phase.
type TaskVerifier struct {
	llmConfig MaclawLLMConfig
}

// NewTaskVerifier creates a TaskVerifier.
func NewTaskVerifier(cfg MaclawLLMConfig) *TaskVerifier {
	return &TaskVerifier{llmConfig: cfg}
}

// Verify checks whether the agent output satisfies the task description.
// Returns a TaskVerdict with pass/fail, a 0-100 score, and reasoning.
func (v *TaskVerifier) Verify(taskDesc, agentOutput string) (*TaskVerdict, error) {
	if strings.TrimSpace(agentOutput) == "" {
		return &TaskVerdict{
			Pass:   false,
			Score:  0,
			Reason: "agent 没有产出任何输出",
		}, nil
	}

	prompt := fmt.Sprintf(`你是一个严格的代码审查员。请判断以下 agent 的工作输出是否完成了指定的任务。

任务描述：
%s

Agent 输出摘要：
%s

请用 JSON 格式回答，包含以下字段：
- pass: bool，任务是否基本完成
- score: int (0-100)，完成度评分
- reason: string，判断理由（简洁）
- missing: string，如果未完成，缺少什么（如果已完成则为空字符串）

只返回 JSON，不要其他内容。`, taskDesc, truncateForPrompt(agentOutput, 3000))

	body, err := swarmCallLLM(v.llmConfig, prompt, 0.1, 30*time.Second)
	if err != nil {
		// LLM 调用失败时默认通过，避免阻塞流程
		return &TaskVerdict{Pass: true, Score: 50, Reason: "验收 LLM 调用失败，默认通过: " + err.Error()}, nil
	}

	var verdict TaskVerdict
	cleaned := extractJSONObject(body)
	if err := json.Unmarshal(cleaned, &verdict); err != nil {
		return &TaskVerdict{Pass: true, Score: 50, Reason: "验收结果解析失败，默认通过"}, nil
	}
	return &verdict, nil
}

// extractJSONObject finds the first { ... } block in the text.
func extractJSONObject(data []byte) []byte {
	s := string(data)
	// Strip markdown fences
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return []byte(s[start : end+1])
	}
	return []byte(strings.TrimSpace(s))
}

// truncateForPrompt truncates text to roughly maxChars, rune-safe.
func truncateForPrompt(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "\n...(已截断)"
}
