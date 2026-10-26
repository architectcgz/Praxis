package dto

import (
	"encoding/json"
	"time"
)

type EntryKind string

const (
	EntryMessage EntryKind = "message"
	EntrySummary EntryKind = "summary"
)

// Entry 是前端会话记录，只包含展示、消息分组和耗时关联所需的数据。
// Kind 取 message 或 summary，不公开内部替代范围、压缩保护规则或模型续传状态。
type Entry struct {
	ID          string       `json:"id"`
	ExecutionID string       `json:"executionId,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	Role        string       `json:"role"`
	Kind        EntryKind    `json:"kind"`
	Content     []EntryBlock `json:"content"`
}

// EntryBlock 只承载可展示的正文、思考和工具活动；不包含 Provider 推理续传数据。
type EntryBlock struct {
	Kind    string          `json:"kind"`
	Text    string          `json:"text,omitempty"`
	CallID  string          `json:"callId,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"isError,omitzero"`
}
