package tools

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ToolCall 表示一个工具调用
type ToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function ToolFunction           `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ParseToolCalls 从 GLM-5 的回复中解析工具调用
// 格式示例：
// ```tool_call
// {
//   "name": "exec",
//   "arguments": {
//     "command": "top -bn1 | head -20"
//   }
// }
// ```
func ParseToolCalls(content string) []ToolCall {
	var toolCalls []ToolCall
	
	// 匹配 ```tool_call ... ``` 代码块
	re := regexp.MustCompile("(?s)```tool_call\\s*\\n(.*?)\\n```")
	matches := re.FindAllStringSubmatch(content, -1)
	
	for i, match := range matches {
		if len(match) < 2 {
			continue
		}
		
		jsonStr := strings.TrimSpace(match[1])
		
		var callData struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		
		if err := json.Unmarshal([]byte(jsonStr), &callData); err != nil {
			continue
		}
		
		// 转换 arguments 为 JSON 字符串
		argsBytes, _ := json.Marshal(callData.Arguments)
		
		toolCalls = append(toolCalls, ToolCall{
			ID:   generateToolCallID(i),
			Type: "function",
			Function: ToolFunction{
				Name:      callData.Name,
				Arguments: string(argsBytes),
			},
		})
	}
	
	return toolCalls
}

// RemoveToolCallBlocks 移除内容中的工具调用代码块
func RemoveToolCallBlocks(content string) string {
	re := regexp.MustCompile("(?s)```tool_call\\s*\\n.*?\\n```")
	return strings.TrimSpace(re.ReplaceAllString(content, ""))
}

// HasToolCalls 检查内容是否包含工具调用
func HasToolCalls(content string) bool {
	return strings.Contains(content, "```tool_call")
}

func generateToolCallID(index int) string {
	return "call_" + randomString(24)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}
