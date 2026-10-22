package dto

import (
	"time"
)

type SessionSnapshot struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	WorkspaceID string          `json:"workspaceId"`
	Title       string          `json:"title"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	Agents      []AgentSnapshot `json:"agents"`
	Tasks       []QueuedTask    `json:"tasks"`
}

type SessionSummary struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	WorkspaceID string    `json:"workspaceId"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreateProjectRequest struct {
	ProjectID   string `json:"projectId"`
	WorkspaceID string `json:"workspaceId"`
	ProjectName string `json:"projectName"`
	Path        string `json:"path"`
	RequestID   string `json:"requestId"`
}

type CreateProjectResponse struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
}

type ProjectSummary struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	DefaultWorkspaceID string `json:"defaultWorkspaceId"`
	Path               string `json:"path"`
	State              string `json:"state"`
}

type CreateSessionRequest struct {
	SessionID         string `json:"sessionId"`
	AgentID           string `json:"agentId"`
	ProjectID         string `json:"projectId"`
	WorkspaceID       string `json:"workspaceId"`
	AgentDefinitionID string `json:"agentDefinitionId"`
	RequestID         string `json:"requestId"`
}

type CreateSessionResponse struct {
	SessionID         string `json:"sessionId"`
	ProjectID         string `json:"projectId"`
	WorkspaceID       string `json:"workspaceId"`
	AgentID           string `json:"agentId"`
	AgentDefinitionID string `json:"agentDefinitionId"`
}

type AgentSnapshot struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	SessionID          string              `json:"sessionId"`
	DefinitionID       string              `json:"definitionId"`
	Profile            string              `json:"profile"`
	State              string              `json:"state"`
	CurrentExecutionID string              `json:"currentExecutionId"`
	ExecutionIDs       []string            `json:"executionIds"`
	Executions         []ExecutionSnapshot `json:"executions"`
	ControlCommandIDs  []string            `json:"controlCommandIds"`
}

// ExecutionSnapshot 返回持久化生命周期与安全的失败码，不暴露凭据或原始错误。
type ExecutionSnapshot struct {
	SourceTaskID   string    `json:"sourceTaskId,omitempty"`
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	Outcome        string    `json:"outcome,omitempty"`
	FailureCode    string    `json:"failureCode,omitempty"`
	FailureMessage string    `json:"failureMessage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	EndedAt        time.Time `json:"endedAt,omitzero"`
}

type ModelOption struct {
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	Label                 string   `json:"label"`
	ProviderName          string   `json:"providerName"`
	ReasoningLevels       []string `json:"reasoningLevels"`
	DefaultReasoningLevel string   `json:"defaultReasoningLevel"`
}

// ProviderConfigOption 携带可编辑的 Provider 元数据和密钥状态。
type ProviderConfigOption struct {
	ID           string `json:"id"`
	ProviderName string `json:"providerName"`
	BaseURL      string `json:"baseURL"`
	ProxyURL     string `json:"proxyURL"`
	HasAPIKey    bool   `json:"hasAPIKey"`
}

type GroupConfigOption struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// ModelConfigOption 携带设置界面编辑模型所需的全部字段。
type ModelConfigOption struct {
	ProviderID            string        `json:"providerId"`
	ModelID               string        `json:"modelId"`
	Label                 string        `json:"label"`
	GroupID               string        `json:"groupId"`
	APIFormat             string        `json:"apiFormat"`
	ContextWindow         int           `json:"contextWindow"`
	MaxOutputTokens       int           `json:"maxOutputTokens"`
	ReasoningLevels       []string      `json:"reasoningLevels"`
	DefaultReasoningLevel string        `json:"defaultReasoningLevel"`
	Thinking              ModelThinking `json:"thinking,omitzero"`
}

// ModelConfigDocument 表示可编辑的模型注册表文档。
type ModelConfigDocument struct {
	Groups    []GroupConfigOption    `json:"groups"`
	Providers []ProviderConfigOption `json:"providers"`
	Models    []ModelConfigOption    `json:"models"`
}

type SaveModelConfigRequest struct {
	Groups    []GroupConfigOption    `json:"groups"`
	Providers []ProviderConfigOption `json:"providers"`
	Models    []ModelConfigOption    `json:"models"`
}

// SelectModelRequest 更新指定会话的选择；模型字段全空表示清除该会话的选择。
type SelectModelRequest struct {
	SessionID      string `json:"sessionId"`
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}

// ModelSelection 只返回会话选择的公开字段，不包含完整模型配置。
type ModelSelection struct {
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}

// SaveModelConfigResponse 通过 validationError 报告用户可修正的配置问题。
// 这些问题不属于调用失败，不映射为 binding 错误码，以便界面展示具体修正信息。
type SaveModelConfigResponse struct {
	Saved           bool   `json:"saved"`
	ValidationError string `json:"validationError,omitempty"`
}

type CreateTaskRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
}

type CreateTaskResponse struct {
	TaskID          string `json:"taskId"`
	SchedulingError string `json:"schedulingError,omitempty"`
}

type PauseAgentRequest struct {
	CommandID         string `json:"commandId"`
	AgentID           string `json:"agentId"`
	TargetExecutionID string `json:"targetExecutionId"`
}

type StopAgentRequest = PauseAgentRequest

type AgentControlResponse struct {
	CommandID         string `json:"commandId"`
	AgentID           string `json:"agentId"`
	TargetExecutionID string `json:"targetExecutionId,omitempty"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	ExistingCommand   bool   `json:"existingCommand"`
	CancellationError string `json:"cancellationError,omitempty"`
}

type CancelTaskRequest struct {
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
}

// QueuedTask 展示内存待办与正文，不携带执行终态。
type QueuedTask struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Sequence  uint64    `json:"sequence"`
	CreatedAt time.Time `json:"createdAt"`
}
