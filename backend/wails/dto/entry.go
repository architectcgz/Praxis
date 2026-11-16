package dto

import (
	"time"
)

type EntryKind string

const (
	EntrySubagentReport EntryKind = "subagent_report"
	EntryInstruction    EntryKind = "instruction"
	EntryMessage        EntryKind = "message"
	EntrySummary        EntryKind = "summary"
	EntryRun            EntryKind = "run"
	EntryModelRequest   EntryKind = "model_request"
)

// Entry 是前端会话记录，只包含展示、消息分组和耗时关联所需的数据。
// Kind 区分消息、摘要和执行结束位置，不公开冻结配置、压缩保护规则或模型续传状态。
type Entry struct {
	ID          string       `json:"id"`
	RunID       string       `json:"runId,omitempty"`
	TurnID      string       `json:"turnId,omitempty"`
	RequestID   string       `json:"requestId,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	Role        string       `json:"role"`
	Kind        EntryKind    `json:"kind"`
	RequestKind string       `json:"requestKind,omitempty"`
	Status      string       `json:"status,omitempty"`
	RetryCount  int          `json:"retryCount,omitempty"`
	Content     []EntryBlock `json:"content"`
	Error       string       `json:"error,omitempty"`
	Timing      *Timing      `json:"timing,omitempty"`
}

// EntryBlock 只承载可展示的正文、思考和工具活动；不包含 Provider 推理续传数据。
type EntryBlock struct {
	Timing  *Timing `json:"timing,omitempty"`
	Kind    string  `json:"kind"`
	Text    string  `json:"text,omitempty"`
	CallID  string  `json:"callId,omitempty"`
	Name    string  `json:"name,omitempty"`
	Input   string  `json:"input,omitempty"`
	IsError bool    `json:"isError,omitzero"`
}
