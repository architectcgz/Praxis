package dto

import "time"

type CreateTaskRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	Prompt    string `json:"prompt" validate:"task_prompt"`
}

type CreateTaskResponse struct {
	TaskID          string `json:"taskId"`
	SchedulingError string `json:"schedulingError,omitempty"`
}

type CancelTaskRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	TaskID    string `json:"taskId" validate:"id"`
}

// QueuedTask 展示内存待办与正文，不携带执行终态。
type QueuedTask struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Sequence  uint64    `json:"sequence"`
	CreatedAt time.Time `json:"createdAt"`
}
