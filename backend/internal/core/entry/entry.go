package entry

import (
	"context"
	"encoding/json"
	"errors"
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

// Entry 是会话记录与模型上下文共用的内部有序条目，不定义前端协议。
type Entry struct {
	HistoryEnd int64      `json:"-"` // 记录的结束位置，由 History 提供。
	Payload    Payload    `json:"-"`
	ID         string     `json:"id"`
	RunID      string     `json:"runId,omitempty"`
	TurnID     string     `json:"turnId,omitempty"`
	AuthorKind AuthorKind `json:"authorKind,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// IsSummary 识别摘要事实；上下文选择按最近摘要及后续历史进行。
func (e Entry) IsSummary() bool {
	_, ok := e.Payload.(Summary)
	return ok
}

// RunOutcome 返回终态条目对应的执行结果，其他条目没有结果。
func (e Entry) RunOutcome() RunOutcome {
	switch e.Payload.(type) {
	case RunCompleted:
		return OutcomeCompleted
	case RunFailed:
		return OutcomeFailed
	case RunInterrupted:
		return OutcomeInterrupted
	case SubagentYielded:
		return OutcomeYielded
	default:
		return ""
	}
}

// IsRunTerminal 判断 Entry 是否是 Run 的终态记录。
func (e Entry) IsRunTerminal() bool {
	return e.RunOutcome() != ""
}

// Dialogue 返回模型可读的消息内容；摘要作为 user 角色的历史资料提供。
func (e Entry) Dialogue() (Role, Content) {
	switch value := e.Payload.(type) {
	case Message:
		return value.Role, value.Content
	case Summary:
		return RoleUser, Content{TextBlock{Text: value.Text}}
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
	e.Payload, err = decodePayload(decoded.Kind, decoded.RawPayload)
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

func (e Entry) MarshalJSON() ([]byte, error) {
	type value Entry
	if e.Payload == nil {
		return nil, errors.New("history entry requires a payload")
	}
	return json.Marshal(struct {
		value
		Kind    string  `json:"kind"`
		Payload Payload `json:"payload"`
	}{
		value:   value(e),
		Kind:    e.Payload.kind(),
		Payload: e.Payload,
	})
}

// AppendEntry 是执行流程唯一的同步事实提交能力，归属在准备时绑定。
type AppendEntry func(context.Context, Entry) (*Entry, bool, error)
