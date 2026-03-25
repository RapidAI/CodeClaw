package util

type StandardRequest struct {
	Surface        string
	RequestedModel string
	ResolvedModel  string
	ResponseModel  string
	Messages       []any
	FinalPrompt    string
	ToolNames      []string
	Stream         bool
	Thinking       bool
	Search         bool
	PassThrough    map[string]any
}

func (r StandardRequest) CompletionPayload(sessionID string) map[string]any {
	payload := map[string]any{
		"chat_session_id":   sessionID,
		"parent_message_id": nil,
		"prompt":            r.FinalPrompt,
		"ref_file_ids":      []any{},
		"thinking_enabled":  r.Thinking,
		"search_enabled":    r.Search,
	}
	for k, v := range r.PassThrough {
		payload[k] = v
	}
	return payload
}
