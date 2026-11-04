package entry

import (
	"bytes"
	"encoding/json"
	"praxis/internal/core/agent/run/timing"
	"praxis/internal/core/session/entry/role"
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

// ContextEntry 是会话记录与模型上下文共用的内部有序条目，不定义前端协议。
type ContextEntry struct {
	// ParentID 连接会话历史节点；命名分支由提交目标和分支 Head 表达。
	ParentID string `json:"parentId,omitempty"`
	// Summary 覆盖所在分支路径中此前的全部上下文，原始 Entry 仍然保留。
	Summary      bool       `json:"summary,omitzero"`
	ID           string     `json:"id"`
	RequestID    string     `json:"requestId,omitempty"`
	HandoffID    string     `json:"handoffId,omitempty"`
	RunID        string     `json:"executionId,omitempty"`
	SourceTaskID string     `json:"sourceTaskId,omitempty"`
	TurnID       string     `json:"turnId,omitempty"`
	StopReason   string     `json:"stopReason,omitempty"`
	AuthorKind   AuthorKind `json:"authorKind,omitempty"`
	AuthorID     string     `json:"authorId,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	// Error 标记未取得完整模型响应的条目，不进入后续模型上下文。
	Error string `json:"error,omitempty"`
	// Timing 随响应、工具回执或摘要提交，不参与 Provider 的上下文编码。
	Timing *timing.Record `json:"timing,omitempty"`

	// Protected 标记本轮请求必须容纳的原文，只用于必要输入的容量检查，不限制轮后摘要。
	Protected bool      `json:"protected,omitempty"`
	Role      role.Role `json:"role"`
	Content   []Block   `json:"content"`
}

// IsSummary 识别覆盖此前上下文的摘要节点。
func (e ContextEntry) IsSummary() bool {
	return e.Summary
}

// Clone 返回 Block 的独立副本，避免跨边界修改共享输入。
func (b Block) Clone() Block {
	b.Input = json.RawMessage(bytes.Clone(b.Input))
	b.ReasoningState = b.ReasoningState.Clone()
	return b
}

// Clone 返回 ContextEntry 的独立副本。
func (e ContextEntry) Clone() ContextEntry {
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

// Values 为请求或展示边界创建值列表，内部载荷仍按只读数据共享。
func Values(entries []*ContextEntry) []ContextEntry {
	values := make([]ContextEntry, len(entries))
	for index, entry := range entries {
		values[index] = *entry
	}
	return values
}
