package dto

import (
	"time"
)

type Timing struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
}

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

type HistoryPage struct {
	HeadEntryID string  `json:"headEntryId"`
	Entries     []Entry `json:"entries"`
	HasMore     bool    `json:"hasMore"`
	NextCursor  string  `json:"nextCursor"`
}

type AgentHistoryRequest struct {
	SessionID     string `json:"sessionId" validate:"session_id"`
	AgentID       string `json:"agentId" validate:"id"`
	HeadEntryID   string `json:"headEntryId" validate:"omitempty,id"`
	BeforeEntryID string `json:"beforeEntryId" validate:"omitempty,id"`
	Limit         int    `json:"limit" validate:"min=1,max=200"`
}

type UsageRequest struct {
	SessionID string   `json:"sessionId" validate:"session_id"`
	TurnIDs   []string `json:"turnIds" validate:"max=2000,dive,id"`
}

// ModelUsage 保留 Provider 未上报计数与已知零值的区别。
type ModelUsage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// ModelUsageRecord 是一次模型请求的持久化用量。
type ModelUsageRecord struct {
	SessionID string     `json:"sessionId"`
	AgentID   string     `json:"agentId"`
	RunID     string     `json:"runId"`
	RequestID string     `json:"requestId"`
	TurnID    string     `json:"turnId,omitempty"`
	Usage     ModelUsage `json:"usage"`
}

// SessionUsageSummary 只返回 Run 累计与已知性，不附带 Turn 明细；未知比例编码为 null。
type SessionUsageSummary struct {
	TotalRequests            uint64   `json:"totalRequests"`
	InputTokens              int64    `json:"inputTokens"`
	OutputTokens             int64    `json:"outputTokens"`
	CacheReadInputTokens     int64    `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64    `json:"cacheCreationInputTokens"`
	ReportedRequests         uint64   `json:"reportedRequests"`
	OutputReports            uint64   `json:"outputReports"`
	CacheReadReports         uint64   `json:"cacheReadReports"`
	CacheCreationReports     uint64   `json:"cacheCreationReports"`
	CacheReadRatio           *float64 `json:"cacheReadRatio"`
	CacheReadComplete        bool     `json:"cacheReadComplete"`
}
