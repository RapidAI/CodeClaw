package main

// llm_types_patch.go — missing type definitions needed by llm_stream.go and app_maclaw_llm.go.

// llmUsage represents token usage in LLM API responses (OpenAI/Anthropic).
type llmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
}
