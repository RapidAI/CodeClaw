package openai

import "ds2api/internal/util"

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any) (string, []string) {
	messages := normalizeOpenAIMessagesForPrompt(messagesRaw)
	toolNames := []string{}
	if tools, ok := toolsRaw.([]any); ok && len(tools) > 0 {
		messages, toolNames = injectToolPrompt(messages, tools)
	}
	return util.MessagesPrepare(messages), toolNames
}
