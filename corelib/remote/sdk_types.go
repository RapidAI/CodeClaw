package remote

import (
	"encoding/json"
	"fmt"
)

// SDKMessage 表示 Claude Code stdout 的任意消息。
type SDKMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	SessionID string               `json:"session_id,omitempty"`
	Message   *SDKAssistantPayload `json:"message,omitempty"`
	Result    *SDKResultPayload    `json:"result,omitempty"`

	ParentToolUseID string                 `json:"parent_tool_use_id,omitempty"`
	Event           map[string]interface{} `json:"event,omitempty"`
}

// SDKAssistantPayload 是 assistant 消息的载荷。
type SDKAssistantPayload struct {
	Role    string            `json:"role,omitempty"`
	Content []SDKContentBlock `json:"content,omitempty"`
}

// SDKContentBlock 是 SDK 消息中的内容块。
type SDKContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`

	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	Source        *SDKImageSource   `json:"source,omitempty"`
	NestedContent []SDKContentBlock `json:"-"`
}

// UnmarshalJSON 自定义反序列化 SDKContentBlock。
func (b *SDKContentBlock) UnmarshalJSON(data []byte) error {
	type Alias SDKContentBlock
	type rawBlock struct {
		Alias
		RawContent json.RawMessage `json:"content,omitempty"`
	}

	var raw rawBlock
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*b = SDKContentBlock(raw.Alias)

	if len(raw.RawContent) == 0 {
		return nil
	}

	switch raw.RawContent[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw.RawContent, &s); err == nil {
			b.Content = s
		}
	case '[':
		var nested []SDKContentBlock
		if err := json.Unmarshal(raw.RawContent, &nested); err == nil {
			b.NestedContent = nested
		}
	}

	return nil
}

// SDKImageSource 表示图片内容块的源数据。
type SDKImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// SDKUserContentPart 表示多部分用户消息中的单个部分。
type SDKUserContentPart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Source *SDKImageSource `json:"source,omitempty"`
}

// SDKResultPayload 是 result 消息的载荷。
type SDKResultPayload struct {
	Duration float64 `json:"duration_ms,omitempty"`
	NumTurns int     `json:"num_turns,omitempty"`
}

// SDKControlRequest 是 Claude Code 请求权限时发送的消息。
type SDKControlRequest struct {
	Type      string                `json:"type"`
	RequestID string                `json:"request_id"`
	Request   SDKControlRequestBody `json:"request"`
}

// SDKControlRequestBody 是控制请求的主体。
type SDKControlRequestBody struct {
	Subtype  string      `json:"subtype"`
	ToolName string      `json:"tool_name,omitempty"`
	Input    interface{} `json:"input,omitempty"`
}

// SDKControlResponse 是发送给 Claude Code 的权限响应。
type SDKControlResponse struct {
	Type     string                 `json:"type"`
	Response SDKControlResponseBody `json:"response"`
}

// SDKControlResponseBody 是控制响应的主体。
type SDKControlResponseBody struct {
	Subtype   string               `json:"subtype"`
	RequestID string               `json:"request_id"`
	Error     string               `json:"error,omitempty"`
	Response  *SDKPermissionResult `json:"response,omitempty"`
}

// SDKPermissionResult 是权限结果。
type SDKPermissionResult struct {
	Behavior     string                 `json:"behavior"`
	UpdatedInput map[string]interface{} `json:"updatedInput,omitempty"`
	Message      string                 `json:"message,omitempty"`
}

// SDKControlCancelRequest 是 Claude Code 取消待处理请求时发送的消息。
type SDKControlCancelRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

// SDKUserInput 是通过 stdin 发送给 Claude Code 的用户消息。
type SDKUserInput struct {
	Type    string         `json:"type"`
	Message SDKUserMessage `json:"message"`
}

// SDKUserMessage 是用户消息。
type SDKUserMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// MarshalJSON 自定义序列化 SDKUserMessage。
func (m SDKUserMessage) MarshalJSON() ([]byte, error) {
	type alias struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	a := alias{Role: m.Role}

	switch v := m.Content.(type) {
	case string:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal string content: %w", err)
		}
		a.Content = raw
	case []SDKUserContentPart:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal multi-part content: %w", err)
		}
		a.Content = raw
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal content: %w", err)
		}
		a.Content = raw
	}

	return json.Marshal(a)
}

// UnmarshalJSON 自定义反序列化 SDKUserMessage。
func (m *SDKUserMessage) UnmarshalJSON(data []byte) error {
	type alias struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}

	m.Role = a.Role

	if len(a.Content) == 0 {
		m.Content = ""
		return nil
	}

	switch a.Content[0] {
	case '"':
		var s string
		if err := json.Unmarshal(a.Content, &s); err != nil {
			return fmt.Errorf("unmarshal string content: %w", err)
		}
		m.Content = s
	case '[':
		var parts []SDKUserContentPart
		if err := json.Unmarshal(a.Content, &parts); err != nil {
			return fmt.Errorf("unmarshal multi-part content: %w", err)
		}
		m.Content = parts
	default:
		m.Content = string(a.Content)
	}

	return nil
}

// SDKInterruptRequest 是发送给 Claude Code 的中断请求。
type SDKInterruptRequest struct {
	Type      string           `json:"type"`
	RequestID string           `json:"request_id"`
	Request   SDKInterruptBody `json:"request"`
}

// SDKInterruptBody 是中断请求的主体。
type SDKInterruptBody struct {
	Subtype string `json:"subtype"`
}
