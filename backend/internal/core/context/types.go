// Package context 定义模型可见上下文的独立类型和构建规则。
package context

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ContextEntryKind 表示运行期上下文条目的来源。
type ContextEntryKind string

const (
	ContextEntryReference      ContextEntryKind = "reference"
	ContextEntrySummary        ContextEntryKind = "summary"
	ContextEntryUserInput      ContextEntryKind = "user_input"
	ContextEntryProviderOutput ContextEntryKind = "provider_output"
	ContextEntryToolResult     ContextEntryKind = "tool_result"
	ContextEntryMCPCall        ContextEntryKind = "mcp_call"
	ContextEntryMCPResult      ContextEntryKind = "mcp_result"
)

// ContextRole 表示上下文条目的参与方。
type ContextRole string

const (
	ContextRoleUser      ContextRole = "user"
	ContextRoleAssistant ContextRole = "assistant"
	ContextRoleTool      ContextRole = "tool"
)

// ContextBlockKind 表示上下文条目中的语义内容。
type ContextBlockKind string

const (
	ContextBlockText           ContextBlockKind = "text"
	ContextBlockThinking       ContextBlockKind = "thinking"
	ContextBlockReasoningState ContextBlockKind = "reasoning_state"
	ContextBlockToolCall       ContextBlockKind = "tool_call"
	ContextBlockToolResult     ContextBlockKind = "tool_result"
	ContextBlockMCPCall        ContextBlockKind = "mcp_call"
	ContextBlockMCPResult      ContextBlockKind = "mcp_result"
)

// ContextBlock 是 Provider 无关的上下文内容；协议 adapter 负责把它编码成具体格式。
type ContextBlock struct {
	Kind           ContextBlockKind
	Text           string
	CallID         string
	Name           string
	Input          json.RawMessage
	IsError        bool
	ReasoningState *ReasoningState `json:",omitempty"`
}

// ContextEntry 是运行期上下文中的一个有序语义条目。
type ContextEntry struct {
	// Protected 要求压缩时保留原文；来源与保护策略由调用方决定。
	Protected bool `json:",omitempty"`
	Kind      ContextEntryKind
	Role      ContextRole
	Content   []ContextBlock
}

// ModelContext 是一次 Provider 请求使用的完整模型上下文。
// 调用方提供有序通用内容，执行中可追加模型输出和工具结果。
type ModelContext struct {
	SystemPrompt string
	Entries      []ContextEntry
}

// Clone 返回 ContextBlock 的独立副本，避免跨边界修改共享输入。
func (b ContextBlock) Clone() ContextBlock {
	b.Input = json.RawMessage(bytes.Clone(b.Input))
	b.ReasoningState = b.ReasoningState.Clone()
	return b
}

// Clone 返回 ContextEntry 的独立副本。
func (e ContextEntry) Clone() ContextEntry {
	content := make([]ContextBlock, len(e.Content))
	for index, block := range e.Content {
		content[index] = block.Clone()
	}
	e.Content = content
	return e
}

// Clone 返回 ModelContext 的独立副本。
func (c ModelContext) Clone() ModelContext {
	entries := make([]ContextEntry, len(c.Entries))
	for index, entry := range c.Entries {
		entries[index] = entry.Clone()
	}
	c.Entries = entries
	return c
}

// Digest 返回当前完整模型上下文的内容摘要。
func (c ModelContext) Digest() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256-" + hex.EncodeToString(digest[:]), nil
}
