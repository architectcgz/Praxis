package dto

import "time"

type MessageKind string

const (
	MessageSubagentReport MessageKind = "subagent_report"
	MessageInstruction    MessageKind = "instruction"
	MessageChat           MessageKind = "message"
	MessageSummary        MessageKind = "summary"
)

// HistoryMessage 承载可展示的对话内容。
type HistoryMessage struct {
	ID        string         `json:"id"`
	RunID     string         `json:"runId"`
	TurnID    string         `json:"turnId,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	Role      string         `json:"role"`
	Kind      MessageKind    `json:"kind"`
	Content   []MessageBlock `json:"content"`
	Error     string         `json:"error,omitempty"`
	Timing    *Timing        `json:"timing,omitempty"`
}

type MessageBlock struct {
	Timing  *Timing `json:"timing,omitempty"`
	Kind    string  `json:"kind"`
	Text    string  `json:"text,omitempty"`
	CallID  string  `json:"callId,omitempty"`
	Name    string  `json:"name,omitempty"`
	Input   string  `json:"input,omitempty"`
	IsError bool    `json:"isError,omitzero"`
}

type Timing struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
}

type HistoryPage struct {
	Messages   []HistoryMessage       `json:"messages"`
	Requests   []ModelRequestActivity `json:"requests"`
	HasMore    bool                   `json:"hasMore"`
	NextCursor *int64                 `json:"nextCursor"`
}

type AgentHistoryRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"id"`
	Before    *int64 `json:"before" validate:"omitempty,min=0"`
	Limit     int    `json:"limit" validate:"min=1,max=200"`
}

// ModelRequestActivity 独立记录一次请求尝试，随历史查询或请求事件交付。
type ModelRequestActivity struct {
	RequestID   string              `json:"requestId"`
	RunID       string              `json:"runId"`
	TurnID      string              `json:"turnId,omitempty"`
	RequestKind string              `json:"requestKind"`
	RetryCount  int                 `json:"retryCount"`
	CreatedAt   time.Time           `json:"createdAt"`
	Timing      *Timing             `json:"timing,omitempty"`
	HistoryEnd  int64               `json:"historyEnd"`
	Result      *ModelRequestResult `json:"result,omitempty"`
}

type ModelRequestResult struct {
	Status     string      `json:"status"`
	Error      string      `json:"error,omitempty"`
	StopReason string      `json:"stopReason,omitempty"`
	Partial    string      `json:"partial,omitempty"`
	Usage      *ModelUsage `json:"usage,omitempty"`
}

type ModelUsage struct {
	InputTokens              *int64 `json:"inputTokens,omitempty"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// SessionUsageSummary 是初始化时的累计；HistoryEnds 标记各 Agent 已计入的位置。
type SessionUsageSummary struct {
	TotalRequests            uint64           `json:"totalRequests"`
	ReportedRequests         uint64           `json:"reportedRequests"`
	InputTokens              int64            `json:"inputTokens"`
	OutputTokens             int64            `json:"outputTokens"`
	CacheReadInputTokens     int64            `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64            `json:"cacheCreationInputTokens"`
	InputReports             uint64           `json:"inputReports"`
	OutputReports            uint64           `json:"outputReports"`
	CacheReadReports         uint64           `json:"cacheReadReports"`
	CacheCreationReports     uint64           `json:"cacheCreationReports"`
	HistoryEnds              map[string]int64 `json:"historyEnds"`
}
