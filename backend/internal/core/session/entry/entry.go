package entry

import (
	"bytes"
	"encoding/json"
	"praxis/internal/core/session/entry/role"
	"slices"
	"strings"
	"time"
)

type AuthorKind string

const (
	AuthorUser   AuthorKind = "user"
	AuthorAgent  AuthorKind = "agent"
	AuthorTool   AuthorKind = "tool"
	AuthorSystem AuthorKind = "system"
)

// BlockKind 表示上下文条目中的语义内容。
type BlockKind string

const (
	BlockText           BlockKind = "text"
	BlockThinking       BlockKind = "thinking"
	BlockReasoningState BlockKind = "reasoning_state"
	BlockToolCall       BlockKind = "tool_call"
	BlockToolResult     BlockKind = "tool_result"
	BlockMCPCall        BlockKind = "mcp_call"
	BlockMCPResult      BlockKind = "mcp_result"
)

// Block 是 Provider 无关的上下文内容；协议 adapter 负责把它编码成具体格式。
type Block struct {
	Kind           BlockKind       `json:"kind"`
	Text           string          `json:"text,omitempty"`
	CallID         string          `json:"callId,omitempty"`
	Name           string          `json:"name,omitempty"`
	Input          json.RawMessage `json:"input,omitempty"`
	IsError        bool            `json:"isError,omitempty"`
	ReasoningState *ReasoningState `json:"reasoningState,omitempty"`
}

// ContextEntry 是 Session 历史、模型上下文和前端展示共用的有序记录。
type ContextEntry struct {
	// Replaces 仅用于摘要，记录被替代的历史 Entry，不修改原始记录。
	Replaces     []string   `json:"replaces,omitempty"`
	ID           string     `json:"id"`
	Sequence     uint64     `json:"sequence"`
	RequestID    string     `json:"requestId,omitempty"`
	HandoffID    string     `json:"handoffId,omitempty"`
	ExecutionID  string     `json:"executionId,omitempty"`
	SourceTaskID string     `json:"sourceTaskId,omitempty"`
	TurnID       string     `json:"turnId,omitempty"`
	StopReason   string     `json:"stopReason,omitempty"`
	AuthorKind   AuthorKind `json:"authorKind,omitempty"`
	AuthorID     string     `json:"authorId,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`

	// Protected 要求压缩时保留原文；来源与保护策略由调用方决定。
	Protected bool      `json:"protected,omitempty"`
	Role      role.Role `json:"role"`
	Content   []Block   `json:"content"`
}

// IsSummary 根据历史替代范围识别压缩摘要。
func (e ContextEntry) IsSummary() bool {
	return len(e.Replaces) > 0
}

// Clone 返回 Block 的独立副本，避免跨边界修改共享输入。
func (b Block) Clone() Block {
	b.Input = json.RawMessage(bytes.Clone(b.Input))
	b.ReasoningState = b.ReasoningState.Clone()
	return b
}

// Clone 返回 ContextEntry 的独立副本。
func (e ContextEntry) Clone() ContextEntry {
	e.Replaces = slices.Clone(e.Replaces)
	if e.Content == nil {
		return e
	}
	content := make([]Block, len(e.Content))
	for index, block := range e.Content {
		content[index] = block.Clone()
	}
	e.Content = content
	return e
}

// TextContent 返回正文块，保留完整历史中的其他内容供各消费方选择。
func (e ContextEntry) TextContent() string {
	var text strings.Builder
	for _, block := range e.Content {
		if block.Kind == BlockText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
