package entry

import (
	"context"
	"encoding/json"
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/core/entry/record"
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

// Entry 与模型请求的部分响应使用同一组内容块类型。
type (
	ContentBlock        = record.ContentBlock
	TextBlock           = record.TextBlock
	ThinkingBlock       = record.ThinkingBlock
	ToolCallBlock       = record.ToolCallBlock
	ToolResultBlock     = record.ToolResultBlock
	ReasoningStateBlock = record.ReasoningStateBlock
)

// Entry 是会话记录与模型上下文共用的内部有序条目，不定义前端协议。
type Entry struct {
	HistoryEnd int64                `json:"-"` // 记录的结束位置，由 History 提供。
	Payload    entryPayload.Payload `json:"-"`
	ID         string               `json:"id"`
	RunID      string               `json:"runId,omitempty"`
	TurnID     string               `json:"turnId,omitempty"`
	AuthorKind AuthorKind           `json:"authorKind,omitempty"`
	CreatedAt  time.Time            `json:"createdAt"`
}

// IsSummary 识别摘要事实；上下文选择按最近摘要及后续历史进行。
func (e Entry) IsSummary() bool {
	_, ok := e.Payload.(entryPayload.Summary)
	return ok
}

// Dialogue 返回模型可读的消息内容；摘要作为 user 角色的历史资料提供。
func (e Entry) Dialogue() (role.Role, []ContentBlock) {
	switch value := e.Payload.(type) {
	case entryPayload.Message:
		return value.Role, value.Content
	case entryPayload.Summary:
		return role.User, value.Content
	default:
		return "", nil
	}
}

// UnmarshalJSON 在持久化读取入口按 Entry 顶层的 kind 解码载荷。
func (e *Entry) UnmarshalJSON(data []byte) error {
	type entry Entry
	decoded := struct {
		*entry
		Kind       string          `json:"kind"`
		RawPayload json.RawMessage `json:"payload"`
	}{entry: (*entry)(e)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var err error
	e.Payload, err = entryPayload.Decode(decoded.Kind, decoded.RawPayload)
	return err
}

// TextContent 返回正文块，保留完整历史中的其他内容供各消费方选择。
func (e Entry) TextContent() string {
	var text strings.Builder
	_, blocks := e.Dialogue()
	for _, block := range blocks {
		if value, ok := block.(TextBlock); ok {
			text.WriteString(value.Text)
		}
	}
	return text.String()
}

// Preview 提供历史目录的短文本，不保存第二份正文。
func (e Entry) Preview() string {
	if terminal, ok := e.Payload.(entryPayload.RunTerminal); ok {
		return "执行结束 · " + string(terminal.Outcome())
	}
	if e.IsSummary() {
		return "上下文摘要"
	}
	text := strings.Join(strings.Fields(e.TextContent()), " ")
	if text == "" {
		messageRole, _ := e.Dialogue()
		text = string(messageRole)
	}
	runes := []rune(text)
	if len(runes) > 100 {
		text = string(runes[:100]) + "…"
	}
	return text
}

// Values 为请求或展示边界创建值列表，内部载荷仍按只读数据共享。
func Values(entries []*Entry) []Entry {
	values := make([]Entry, len(entries))
	for index, entry := range entries {
		values[index] = *entry
	}
	return values
}

func (e Entry) MarshalJSON() ([]byte, error) {
	type value Entry
	kind, payload, err := entryPayload.Encode(e.Payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		value
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}{
		value:   value(e),
		Kind:    kind,
		Payload: payload,
	})
}

// ContributesToModelContext 判断 Entry 是否会进入下一次模型请求。
func (e Entry) ContributesToModelContext() bool {
	messageRole, _ := e.Dialogue()
	return messageRole == role.User || messageRole == role.Assistant || messageRole == role.Tool
}

// AppendEntry 是执行流程唯一的同步事实提交能力，归属在准备时绑定。
type AppendEntry func(context.Context, Entry) (*Entry, bool, error)
