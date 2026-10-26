package dto

import "time"

// OperationTiming 展示 Execution 总耗时及其操作计时；可选耗时为 nil 时保持未知。
type OperationTiming struct {
	SessionID       string    `json:"sessionId"`
	AgentID         string    `json:"agentId"`
	ExecutionID     string    `json:"executionId"`
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
