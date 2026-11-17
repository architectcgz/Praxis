package dto

import (
	"time"
)

type CreateProjectRequest struct {
	ProjectID   string `json:"projectId" validate:"project_id"`
	ProjectName string `json:"projectName" validate:"project_name"`
	Path        string `json:"path" validate:"project_path"`
	RequestID   string `json:"requestId" validate:"id"`
}

type CreateProjectResponse struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
}

type ProjectSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

type SessionSnapshot struct {
	QueuePaused bool                   `json:"queuePaused"`
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"projectId"`
	Title       string                 `json:"title"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	Agents      []AgentView            `json:"agents"`
	Tasks       []QueuedTask           `json:"tasks"`
	Entries     map[string]HistoryPage `json:"entries"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateSessionRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	ProjectID string `json:"projectId" validate:"project_id"`
	RequestID string `json:"requestId" validate:"id"`
}

type CreateSessionResponse struct {
	SessionID string `json:"sessionId"`
	ProjectID string `json:"projectId"`
}

type ModelOption struct {
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	Label                 string   `json:"label"`
	ProviderName          string   `json:"providerName"`
	ReasoningLevels       []string `json:"reasoningLevels"`
	DefaultReasoningLevel string   `json:"defaultReasoningLevel"`
}

// SelectModelRequest 只向指定会话提交完整的模型选择。
type SelectModelRequest struct {
	SessionID      string `json:"sessionId" validate:"session_id"`
	ProviderID     string `json:"providerId" validate:"required,config_id"`
	ModelID        string `json:"modelId" validate:"required,id"`
	ReasoningLevel string `json:"reasoningLevel" validate:"omitempty,id"`
}

// ModelSelection 只返回会话选择的公开字段，不包含完整模型配置。
type ModelSelection struct {
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}

type CreateTaskRequest struct {
	SubmissionID string `json:"submissionId" validate:"id"`
	SessionID    string `json:"sessionId" validate:"session_id"`
	Prompt       string `json:"prompt" validate:"task_prompt"`
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
