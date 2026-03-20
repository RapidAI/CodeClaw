package security

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LLMSecurityCaller abstracts the LLM call for security review.
type LLMSecurityCaller interface {
	// SecurityReview sends a security review prompt and returns the response text.
	SecurityReview(systemPrompt, userPrompt string) (string, error)
	// IsConfigured reports whether the LLM backend is ready.
	IsConfigured() bool
}

// LLMReview performs LLM-assisted security review on tool invocations.
type LLMReview struct {
	llm LLMSecurityCaller
}

// NewLLMReview creates a new LLMReview.
func NewLLMReview(llm LLMSecurityCaller) *LLMReview {
	return &LLMReview{llm: llm}
}

// Review performs an LLM security review on the given risk context.
func (r *LLMReview) Review(ctx RiskContext, assessment RiskAssessment) (LLMSecurityVerdict, string, error) {
	if r.llm == nil || !r.llm.IsConfigured() {
		return VerdictSafe, "LLM not configured, skipping security review", nil
	}

	verdict, explanation, err := r.callLLM(ctx, assessment)
	if err != nil {
		fbVerdict, fbReason := RuleBasedFallback(assessment.Level)
		return fbVerdict, fmt.Sprintf("LLM review failed (%v), fallback: %s", err, fbReason), nil
	}
	return verdict, explanation, nil
}

func (r *LLMReview) callLLM(riskCtx RiskContext, assessment RiskAssessment) (LLMSecurityVerdict, string, error) {
	systemPrompt := "You are a security reviewer for an AI coding assistant. Evaluate the safety of tool calls and respond with a JSON object containing \"verdict\" (one of \"safe\", \"risky\", \"dangerous\") and \"explanation\" (a brief reason)."
	userPrompt := BuildSecurityPrompt(riskCtx, assessment)

	content, err := r.llm.SecurityReview(systemPrompt, userPrompt)
	if err != nil {
		return "", "", err
	}
	return ParseSecurityVerdict(content)
}

// ParseSecurityVerdict extracts the verdict from LLM response content.
func ParseSecurityVerdict(content string) (LLMSecurityVerdict, string, error) {
	content = strings.TrimSpace(content)

	var verdictResp struct {
		Verdict     string `json:"verdict"`
		Explanation string `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(content), &verdictResp); err == nil {
		v := normalizeVerdict(verdictResp.Verdict)
		if v != "" {
			return v, verdictResp.Explanation, nil
		}
	}

	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "dangerous"):
		return VerdictDangerous, content, nil
	case strings.Contains(lower, "risky"):
		return VerdictRisky, content, nil
	case strings.Contains(lower, "safe"):
		return VerdictSafe, content, nil
	default:
		return VerdictRisky, content, nil
	}
}

func normalizeVerdict(s string) LLMSecurityVerdict {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "safe":
		return VerdictSafe
	case "risky":
		return VerdictRisky
	case "dangerous":
		return VerdictDangerous
	default:
		return ""
	}
}

// BuildSecurityPrompt constructs the prompt for LLM security review.
func BuildSecurityPrompt(ctx RiskContext, assessment RiskAssessment) string {
	var sb strings.Builder
	sb.WriteString("Please evaluate the safety of the following tool call:\n\n")
	sb.WriteString(fmt.Sprintf("Tool: %s\n", ctx.ToolName))
	sb.WriteString(fmt.Sprintf("Session: %s\n", ctx.SessionID))
	sb.WriteString(fmt.Sprintf("Project Path: %s\n", ctx.ProjectPath))
	sb.WriteString(fmt.Sprintf("Permission Mode: %s\n", ctx.PermissionMode))
	sb.WriteString(fmt.Sprintf("Consecutive Call Count: %d\n", ctx.CallCount))
	sb.WriteString(fmt.Sprintf("Risk Level: %s\n", assessment.Level))
	sb.WriteString(fmt.Sprintf("Risk Reason: %s\n", assessment.Reason))

	if len(ctx.Arguments) > 0 {
		argsJSON, err := json.Marshal(ctx.Arguments)
		if err == nil {
			sb.WriteString(fmt.Sprintf("Arguments: %s\n", string(argsJSON)))
		}
	}
	sb.WriteString("\nRespond with a JSON object: {\"verdict\": \"safe|risky|dangerous\", \"explanation\": \"...\"}")
	return sb.String()
}

// RuleBasedFallback maps a RiskLevel to a verdict when LLM is unavailable.
func RuleBasedFallback(level RiskLevel) (LLMSecurityVerdict, string) {
	switch level {
	case RiskCritical:
		return VerdictDangerous, "critical risk level mapped to dangerous"
	case RiskHigh:
		return VerdictRisky, "high risk level mapped to risky"
	case RiskMedium:
		return VerdictRisky, "medium risk level mapped to risky"
	default:
		return VerdictSafe, "low risk level mapped to safe"
	}
}
