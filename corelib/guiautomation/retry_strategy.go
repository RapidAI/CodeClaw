package guiautomation

import (
	"fmt"
	"strings"
	"time"
)

// GUIFailureType classifies a GUI step failure.
type GUIFailureType int

const (
	GUIFailureElementNotFound GUIFailureType = iota
	GUIFailureTimeout
	GUIFailureUnknown
)

// GUIRetryDecision describes how to handle a failed step.
type GUIRetryDecision struct {
	ShouldRetry     bool
	WaitBefore      time.Duration
	AdjustedTimeout time.Duration
	NeedsLLM        bool
	LLMContext      string
	Reason          string
}

// GUIRetryStrategy decides how to handle GUI step failures.
type GUIRetryStrategy struct {
	MaxRetries int // default 3
}

// NewGUIRetryStrategy creates a retry strategy with the given max retries.
func NewGUIRetryStrategy(maxRetries int) *GUIRetryStrategy {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &GUIRetryStrategy{MaxRetries: maxRetries}
}

// Decide returns a retry decision based on the failure type, step context, and retry count.
// Logic: count<3 allow retry, count>=3 deny; element-not-found increases wait;
// timeout extends timeout; retry 2 triggers LLM context.
func (r *GUIRetryStrategy) Decide(failure GUIFailureType, step GUIStepSpec, retryCount int, screenshotB64, ocrText string) *GUIRetryDecision {
	if retryCount >= r.MaxRetries {
		return &GUIRetryDecision{
			ShouldRetry: false,
			Reason:      fmt.Sprintf("exceeded max retries (%d)", r.MaxRetries),
		}
	}

	switch failure {
	case GUIFailureElementNotFound:
		return r.decideElementNotFound(step, retryCount, screenshotB64, ocrText)
	case GUIFailureTimeout:
		return r.decideTimeout(step, retryCount)
	default:
		return r.decideUnknown(step, retryCount, screenshotB64, ocrText)
	}
}

// ClassifyFailure infers a GUIFailureType from an error.
func (r *GUIRetryStrategy) ClassifyFailure(err error) GUIFailureType {
	if err == nil {
		return GUIFailureUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found") || strings.Contains(msg, "no element"):
		return GUIFailureElementNotFound
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		return GUIFailureTimeout
	default:
		return GUIFailureUnknown
	}
}

func (r *GUIRetryStrategy) decideElementNotFound(step GUIStepSpec, count int, screenshotB64, ocrText string) *GUIRetryDecision {
	d := &GUIRetryDecision{ShouldRetry: true}
	// Increase wait time for element-not-found
	switch count {
	case 0:
		d.WaitBefore = 3 * time.Second
		d.Reason = "element not found, waiting 3s before retry"
	case 1:
		d.WaitBefore = 6 * time.Second
		d.Reason = "element not found, waiting 6s before retry"
	default:
		// retry 2 triggers LLM context
		d.WaitBefore = 2 * time.Second
		d.NeedsLLM = true
		d.LLMContext = r.buildLLMContext("element_not_found", step, screenshotB64, ocrText)
		d.Reason = "element not found after multiple retries, requesting LLM assistance"
	}
	return d
}

func (r *GUIRetryStrategy) decideTimeout(step GUIStepSpec, count int) *GUIRetryDecision {
	d := &GUIRetryDecision{ShouldRetry: true}
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	// Extend timeout on each retry
	switch count {
	case 0:
		d.AdjustedTimeout = timeout * 2
		d.Reason = "timeout, doubling step timeout"
	case 1:
		d.AdjustedTimeout = timeout * 3
		d.Reason = "timeout, tripling step timeout"
	default:
		d.AdjustedTimeout = timeout * 3
		d.NeedsLLM = true
		d.LLMContext = r.buildLLMContext("timeout", step, "", "")
		d.Reason = "timeout after multiple retries, requesting LLM assistance"
	}
	return d
}

func (r *GUIRetryStrategy) decideUnknown(step GUIStepSpec, count int, screenshotB64, ocrText string) *GUIRetryDecision {
	d := &GUIRetryDecision{
		ShouldRetry: true,
		WaitBefore:  2 * time.Second,
		Reason:      "unknown failure, retrying after short wait",
	}
	if count >= 2 {
		d.NeedsLLM = true
		d.LLMContext = r.buildLLMContext("unknown", step, screenshotB64, ocrText)
		d.Reason = "unknown failure after multiple retries, requesting LLM assistance"
	}
	return d
}

func (r *GUIRetryStrategy) buildLLMContext(failureKind string, step GUIStepSpec, screenshotB64, ocrText string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("GUI task step failed (type: %s)\n", failureKind))
	b.WriteString(fmt.Sprintf("Action: %s, Params: %v\n", step.Action, step.Params))
	if ocrText != "" {
		b.WriteString(fmt.Sprintf("OCR text on screen:\n%s\n", ocrText))
	}
	if screenshotB64 != "" {
		b.WriteString("[screenshot attached]\n")
	}
	b.WriteString("Please suggest the next action based on the above context.")
	return b.String()
}
