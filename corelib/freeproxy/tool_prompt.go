package freeproxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateToolSystemPrompt builds a system prompt that teaches the model to
// use ```tool_call blocks for the given OpenAI-format tools list.
// If tools is empty/nil, returns "".
func GenerateToolSystemPrompt(tools []interface{}) string {
	if len(tools) == 0 {
		return ""
	}
	var descs []string
	for _, t := range tools {
		b, err := json.Marshal(t)
		if err != nil {
			continue
		}
		var tm map[string]interface{}
		if err := json.Unmarshal(b, &tm); err != nil {
			continue
		}
		if tm["type"] != "function" {
			continue
		}
		fn, _ := tm["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if name == "" {
			continue
		}
		if len(desc) > 60 {
			desc = desc[:60] + "..."
		}
		entry := fmt.Sprintf("- %s: %s", name, desc)
		// Include parameter schema if available
		if params, ok := fn["parameters"]; ok {
			if pb, err := json.Marshal(params); err == nil {
				entry += fmt.Sprintf("\n  参数: %s", string(pb))
			}
		}
		descs = append(descs, entry)
	}
	if len(descs) == 0 {
		return ""
	}

	return `你是智能助手。需要执行操作时，使用以下格式调用工具：

` + "```tool_call" + `
{"name":"工具名","arguments":{"参数":"值"}}
` + "```" + `

可用工具：
` + strings.Join(descs, "\n") + `

规则：
1. 先调用工具获取数据，再基于结果回复用户
2. 简洁直接，不说废话
3. 每次只调用一个工具
4. 工具调用必须严格使用上述格式`
}
