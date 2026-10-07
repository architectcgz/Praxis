package dto

import (
	"encoding/json"
	"time"
)

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
	TaskID    string     `json:"taskId"`
	TurnID    string     `json:"turnId"`
	Usage     ModelUsage `json:"usage"`
}

// OperationTiming 是独立的操作计时事实；可选耗时为 nil 时保持未知。
type OperationTiming struct {
	SessionID       string    `json:"sessionId"`
	AgentID         string    `json:"agentId"`
	TaskID          string    `json:"taskId"`
	Kind            string    `json:"kind"`
	Name            string    `json:"name"`
	ReferenceID     string    `json:"referenceId,omitempty"`
	ID              string    `json:"id"`
	ParentID        string    `json:"parentId,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt,omitzero"`
	DurationMS      *int64    `json:"durationMs,omitempty"`
	FirstResponseMS *int64    `json:"firstResponseMs,omitempty"`
	Status          string    `json:"status"`
	Revision        int       `json:"revision"`
}

// AgentEvent 是 Wails 事件专用 DTO，不携带 runtime、core 或 timing 类型。
type AgentEvent struct {
	Kind           string           `json:"kind"`
	SessionID      string           `json:"sessionId,omitempty"`
	AgentID        string           `json:"agentId"`
	TaskID         string           `json:"taskId"`
	Outcome        string           `json:"outcome,omitempty"`
	FailureCode    string           `json:"failureCode,omitempty"`
	FailureMessage string           `json:"failureMessage,omitempty"`
	TurnID         string           `json:"turnId,omitempty"`
	Text           string           `json:"text,omitempty"`
	CallID         string           `json:"callId,omitempty"`
	Name           string           `json:"name,omitempty"`
	Input          json.RawMessage  `json:"input,omitempty"`
	Result         string           `json:"result,omitempty"`
	IsError        bool             `json:"isError,omitzero"`
	Error          string           `json:"error,omitempty"`
	Timing         *OperationTiming `json:"timing,omitempty"`
	Usage          *ModelUsage      `json:"usage,omitempty"`
}
