package entry

import (
	"context"
	"encoding/json"
	"praxis/internal/core/entry/role"
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
	Payload     Payload        `json:"-"`
	ID          string         `json:"id"`
	RunID       string         `json:"runId,omitempty"`
	TurnID      string         `json:"turnId,omitempty"`
	Response    *ModelResponse `json:"response,omitempty"`
	Compression *Compression   `json:"compression,omitempty"`
	AuthorKind  AuthorKind     `json:"authorKind,omitempty"`
	AuthorID    string         `json:"authorId,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`

	Role    role.Role      `json:"role"`
	Content []ContentBlock `json:"content"`
}

// IsSummary 识别摘要事实；上下文选择按最近摘要及后续历史进行。
func (e ContextEntry) IsSummary() bool {
	return e.Compression != nil
}

// UnmarshalJSON 在持久化读取入口按 kind 解码内容，内部直接使用具体类型。
func (e *ContextEntry) UnmarshalJSON(data []byte) error {
	type entry ContextEntry
	decoded := struct {
		*entry
		RawContent []json.RawMessage `json:"content"`
		RawPayload json.RawMessage   `json:"payload"`
	}{entry: (*entry)(e)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var err error
	e.Payload, err = decodePayload(decoded.RawPayload)
	if err != nil {
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
	if p, ok := e.Payload.(UserInput); ok {
		e.Role = role.User
		e.AuthorKind = AuthorUser
		e.Content = []ContentBlock{TextBlock{Text: p.Text}}
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

func (e ContextEntry) MarshalJSON() ([]byte, error) {
	type value ContextEntry
	payload, err := encodePayload(e.Payload)
	if err != nil {
		return nil, err
	}
	if _, ok := e.Payload.(UserInput); ok {
		e.Content = nil
	}
	return json.Marshal(struct {
		value
		Payload json.RawMessage `json:"payload,omitempty"`
	}{value: value(e), Payload: payload})
}

// AppendEntry 是执行流程唯一的同步事实提交能力，归属在准备时绑定。
type AppendEntry func(context.Context, ContextEntry) (*ContextEntry, bool, error)
