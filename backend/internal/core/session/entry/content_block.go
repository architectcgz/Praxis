package entry

import (
	"encoding/json"
	"fmt"
	"praxis/internal/core/tool"
)

// ContentBlock 是消息与模型流事件共用的内容，只允许本包定义的具体类型。
type ContentBlock interface {
	contentBlock()
}

type TextBlock struct {
	Text string `json:"text"`
}

type ThinkingBlock struct {
	Text string `json:"text"`
}

type ToolCallBlock struct {
	CallID    string        `json:"callId"`
	Name      tool.ToolName `json:"name"`
	Arguments string        `json:"input"`
}

type ToolResultBlock struct {
	CallID  string        `json:"callId"`
	Name    tool.ToolName `json:"name"`
	Text    string        `json:"text"`
	IsError bool          `json:"isError,omitzero"`
}

type ReasoningStateBlock struct {
	State *ReasoningState `json:"reasoningState"`
}

func (TextBlock) contentBlock()           {}
func (ThinkingBlock) contentBlock()       {}
func (ToolCallBlock) contentBlock()       {}
func (ToolResultBlock) contentBlock()     {}
func (ReasoningStateBlock) contentBlock() {}

func (b TextBlock) MarshalJSON() ([]byte, error) {
	type value TextBlock
	return json.Marshal(struct {
		Kind string `json:"kind"`
		value
	}{Kind: "text", value: value(b)})
}

func (b ThinkingBlock) MarshalJSON() ([]byte, error) {
	type value ThinkingBlock
	return json.Marshal(struct {
		Kind string `json:"kind"`
		value
	}{Kind: "thinking", value: value(b)})
}

func (b ToolCallBlock) MarshalJSON() ([]byte, error) {
	type value ToolCallBlock
	return json.Marshal(struct {
		Kind string `json:"kind"`
		value
	}{Kind: "tool_call", value: value(b)})
}

// UnmarshalJSON 接受参数原文字符串或结构化 JSON 值，内存中统一保留原文。
func (b *ToolCallBlock) UnmarshalJSON(data []byte) error {
	type value ToolCallBlock
	decoded := struct {
		*value
		Input json.RawMessage `json:"input"`
	}{value: (*value)(b)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if len(decoded.Input) > 0 && decoded.Input[0] == '"' {
		return json.Unmarshal(decoded.Input, &b.Arguments)
	}
	b.Arguments = string(decoded.Input)
	return nil
}

func (b ToolResultBlock) MarshalJSON() ([]byte, error) {
	type value ToolResultBlock
	return json.Marshal(struct {
		Kind string `json:"kind"`
		value
	}{Kind: "tool_result", value: value(b)})
}

func (b ReasoningStateBlock) MarshalJSON() ([]byte, error) {
	type value ReasoningStateBlock
	return json.Marshal(struct {
		Kind string `json:"kind"`
		value
	}{Kind: "reasoning_state", value: value(b)})
}

func decodeContentBlock(data json.RawMessage) (ContentBlock, error) {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	switch header.Kind {
	case "text":
		return decodeBlock[TextBlock](data)
	case "thinking":
		return decodeBlock[ThinkingBlock](data)
	case "tool_call":
		return decodeBlock[ToolCallBlock](data)
	case "tool_result":
		return decodeBlock[ToolResultBlock](data)
	case "reasoning_state":
		return decodeBlock[ReasoningStateBlock](data)
	default:
		return nil, fmt.Errorf("unknown content block kind %q", header.Kind)
	}
}

func decodeBlock[T ContentBlock](data json.RawMessage) (ContentBlock, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
