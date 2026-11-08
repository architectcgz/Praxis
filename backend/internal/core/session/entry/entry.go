package entry

import (
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
	Protected bool           `json:"protected,omitempty"`
	Role      role.Role      `json:"role"`
	Content   []ContentBlock `json:"content"`
}

// IsSummary 识别覆盖此前上下文的摘要节点。
func (e ContextEntry) IsSummary() bool {
	return e.Summary
}

// UnmarshalJSON 在持久化读取入口按 kind 解码内容，内部直接使用具体类型。
func (e *ContextEntry) UnmarshalJSON(data []byte) error {
	type entry ContextEntry
	decoded := struct {
		*entry
		RawContent []json.RawMessage `json:"content"`
	}{entry: (*entry)(e)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	e.Content = make([]ContentBlock, len(decoded.RawContent))
	for index, raw := range decoded.RawContent {
		block, err := decodeContentBlock(raw)
		if err != nil {
			return err
		}
		e.Content[index] = block
	}
	return nil
}

// Clone 返回 ContextEntry 的独立副本。
func (e ContextEntry) Clone() ContextEntry {
	if e.Content == nil {
		return e
	}
	content := make([]ContentBlock, len(e.Content))
	for index, block := range e.Content {
		if state, ok := block.(ReasoningStateBlock); ok {
			state.State = state.State.Clone()
			block = state
		}
		content[index] = block
	}
	e.Content = content
	return e
}

// TextContent 返回正文块，保留完整历史中的其他内容供各消费方选择。
func (e ContextEntry) TextContent() string {
	var text strings.Builder
	for _, block := range e.Content {
		if value, ok := block.(TextBlock); ok {
			text.WriteString(value.Text)
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
