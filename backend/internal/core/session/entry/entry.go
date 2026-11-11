package entry

import (
	"encoding/json"
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
	ParentID    string         `json:"parentId,omitempty"`
	ID          string         `json:"id"`
	RunID       string         `json:"executionId,omitempty"`
	TurnID      string         `json:"turnId,omitempty"`
	Response    *ModelResponse `json:"response,omitempty"`
	Compression *Compression   `json:"compression,omitempty"`
	RunResult   *RunResult     `json:"runResult,omitempty"`
	AuthorKind  AuthorKind     `json:"authorKind,omitempty"`
	AuthorID    string         `json:"authorId,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`

	Role    role.Role      `json:"role"`
	Content []ContentBlock `json:"content"`
}

// IsSummary 识别摘要节点；压缩时保留的原文由 Compression 关联。
func (e ContextEntry) IsSummary() bool {
	return e.Compression != nil
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
