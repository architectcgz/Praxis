package dto

import (
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/core/timing"
	"time"
)

type MessageKind string

const (
	MessageSubagentReport MessageKind = "subagent_report"
	MessageInstruction    MessageKind = "instruction"
	MessageChat           MessageKind = "message"
	MessageSummary        MessageKind = "summary"
	MessageRequestError   MessageKind = "model_request"
)

// HistoryMessage 只承载对话展示字段，不暴露内部事实载荷、配置或推理续传状态。
type HistoryMessage struct {
	ID          string          `json:"id"`
	RunID       string          `json:"runId,omitempty"`
	TurnID      string          `json:"turnId,omitempty"`
	RequestID   string          `json:"requestId,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	Role        string          `json:"role"`
	Kind        MessageKind     `json:"kind"`
	RequestKind string          `json:"requestKind,omitempty"`
	Status      string          `json:"status,omitempty"`
	RetryCount  int             `json:"retryCount,omitzero"`
	Content     []MessageBlock  `json:"content"`
	Error       string          `json:"error,omitempty"`
	Timing      *timing.Record  `json:"timing,omitempty"`
}

// MessageBlock 只包含可展示的正文、思考和工具活动。
type MessageBlock struct {
	Timing  *timing.Record `json:"timing,omitempty"`
	Kind    string         `json:"kind"`
	Text    string         `json:"text,omitempty"`
	CallID  string         `json:"callId,omitempty"`
	Name    string         `json:"name,omitempty"`
	Input   string         `json:"input,omitempty"`
	IsError bool           `json:"isError,omitzero"`
}

type HistoryPage struct {
	Messages   []HistoryMessage   `json:"messages"`
	Usage      []ModelUsageRecord `json:"usage"`
	HasMore    bool               `json:"hasMore"`
	NextCursor *int64             `json:"nextCursor"`
}

type AgentHistoryRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"id"`
	Before    *int64 `json:"before" validate:"omitempty,min=0"`
	Limit     int    `json:"limit" validate:"min=1,max=200"`
}

// ModelUsageRecord 随历史页返回，请求运行中的用量直接随结果事件交付。
type ModelUsageRecord struct {
	RunID     string           `json:"runId"`
	RequestID string           `json:"requestId"`
	TurnID    string           `json:"turnId"`
	Usage     modelusage.Usage `json:"usage"`
}

// SessionUsageSummary 是会话初始化时的历史累计，运行期由请求事件增量更新。
type SessionUsageSummary struct {
	modelusage.Totals
	TotalRequests uint64 `json:"totalRequests"`
}
