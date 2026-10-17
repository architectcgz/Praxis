package session

import (
	"encoding/json"
	"strings"
	"time"

	"praxis/internal/contracts"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type AuthorKind string

const (
	AuthorUser   AuthorKind = "user"
	AuthorAgent  AuthorKind = "agent"
	AuthorTool   AuthorKind = "tool"
	AuthorSystem AuthorKind = "system"
)

type BlockKind string

const (
	BlockText           BlockKind = "text"
	BlockThinking       BlockKind = "thinking"
	BlockReasoningState BlockKind = "reasoning_state"
	BlockToolCall       BlockKind = "tool_call"
	BlockToolResult     BlockKind = "tool_result"
)

type Block struct {
	Kind           BlockKind
	Text           string
	CallID         string
	Name           string
	Input          json.RawMessage `json:",omitempty"`
	IsError        bool
	ReasoningState *contracts.ReasoningState `json:",omitempty"`
}

type MessageData struct {
	ID         string
	Sequence   uint64
	RequestID  string
	HandoffID  string
	TaskID     string
	TurnID     string
	StopReason string
	Role       Role
	AuthorKind AuthorKind
	AuthorID   string
	Blocks     []Block
	CreatedAt  time.Time
}

// TextContent 返回消息中的文本块拼接结果；思考、工具调用和工具结果不属于正文。
func (m MessageData) TextContent() string {
	var content strings.Builder
	for _, block := range m.Blocks {
		if block.Kind == BlockText {
			content.WriteString(block.Text)
		}
	}
	return content.String()
}
