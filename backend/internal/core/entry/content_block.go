package entry

import (
	"encoding/json"
	"fmt"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/timing"
	"praxis/internal/core/tool"
)

// ContentBlock 是消息与模型流事件共用的内容，只允许本包定义的具体类型。
type ContentBlock interface {
	contentBlock()
}

const (
	kindText           = "text"
	kindThinking       = "thinking"
	kindToolCall       = "tool_call"
	kindToolResult     = "tool_result"
	kindReasoningState = "reasoning_state"
)

type blockHeader struct {
	Kind string `json:"kind"`
}

type Content []ContentBlock

func (c Content) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}
	blocks := make([]any, len(c))
	for index, block := range c {
		switch value := block.(type) {
		case TextBlock:
			blocks[index] = struct {
				blockHeader
				TextBlock
			}{blockHeader{Kind: kindText}, value}
		case ThinkingBlock:
			blocks[index] = struct {
				blockHeader
				ThinkingBlock
			}{blockHeader{Kind: kindThinking}, value}
		case ToolCallBlock:
			blocks[index] = struct {
				blockHeader
				ToolCallBlock
			}{blockHeader{Kind: kindToolCall}, value}
		case ToolResultBlock:
			blocks[index] = struct {
				blockHeader
				ToolResultBlock
			}{blockHeader{Kind: kindToolResult}, value}
		case ReasoningStateBlock:
			blocks[index] = struct {
				blockHeader
				ReasoningStateBlock
			}{blockHeader{Kind: kindReasoningState}, value}
		default:
			return nil, fmt.Errorf("unsupported content block type %T", block)
		}
	}
	return json.Marshal(blocks)
}

func (c *Content) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	blocks := make(Content, len(raw))
	for index, value := range raw {
		block, err := decodeContentBlock(value)
		if err != nil {
			return err
		}
		blocks[index] = block
	}
	*c = blocks
	return nil
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
	CallID  string         `json:"callId"`
	Name    tool.ToolName  `json:"name"`
	Text    string         `json:"text"`
	IsError bool           `json:"isError,omitzero"`
	Timing  *timing.Record `json:"timing,omitempty"`
}

type ReasoningStateBlock struct {
	State *modelapi.ReasoningState `json:"reasoningState"`
}

func (TextBlock) contentBlock()           {}
func (ThinkingBlock) contentBlock()       {}
func (ToolCallBlock) contentBlock()       {}
func (ToolResultBlock) contentBlock()     {}
func (ReasoningStateBlock) contentBlock() {}

func decodeContentBlock(data json.RawMessage) (ContentBlock, error) {
	var header blockHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	switch header.Kind {
	case kindText:
		return decodeBlock[TextBlock](data)
	case kindThinking:
		return decodeBlock[ThinkingBlock](data)
	case kindToolCall:
		return decodeBlock[ToolCallBlock](data)
	case kindToolResult:
		return decodeBlock[ToolResultBlock](data)
	case kindReasoningState:
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
