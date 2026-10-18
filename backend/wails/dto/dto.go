package dto

import (
	"encoding/json"
	"time"

	modelcontracts "praxis/internal/app/model/contracts"
)

type SessionSnapshot struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	WorkspaceID string          `json:"workspaceId"`
	Title       string          `json:"title"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	Agents      []AgentSnapshot `json:"agents"`
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
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	SessionID         string         `json:"sessionId"`
	DefinitionID      string         `json:"definitionId"`
	Profile           string         `json:"profile"`
	State             string         `json:"state"`
	CurrentTask       string         `json:"currentTaskId"`
	TaskIDs           []string       `json:"taskIds"`
	Tasks             []TaskSnapshot `json:"tasks"`
	ControlCommandIDs []string       `json:"controlCommandIds"`
}

// TaskSnapshot 返回持久化生命周期与安全的失败码，不暴露凭据或原始错误。
type TaskSnapshot struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	Outcome        string    `json:"outcome,omitempty"`
	FailureCode    string    `json:"failureCode,omitempty"`
	FailureMessage string    `json:"failureMessage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	EndedAt        time.Time `json:"endedAt,omitzero"`
}

type AgentMessage struct {
	ID         string              `json:"id"`
	Sequence   uint64              `json:"sequence"`
	At         time.Time           `json:"at"`
	TaskID     string              `json:"taskId"`
	Role       string              `json:"role"`
	AuthorKind string              `json:"authorKind"`
	AuthorID   string              `json:"authorId,omitempty"`
	Content    string              `json:"content"`
	Thinking   string              `json:"thinking,omitempty"`
	Blocks     []AgentMessageBlock `json:"blocks,omitempty"`
}

// AgentMessageBlock 是消息中可安全展示的结构化内容块。
type AgentMessageBlock struct {
	Kind    string          `json:"kind"`
	Text    string          `json:"text,omitempty"`
	CallID  string          `json:"callId,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"isError,omitzero"`
}

type AgentHistoryItem struct {
	Kind     string        `json:"kind"`
	Sequence uint64        `json:"sequence,omitempty"`
	At       time.Time     `json:"at"`
	Message  *AgentMessage `json:"message,omitempty"`
	Task     *TaskSnapshot `json:"task,omitempty"`
}

type AgentHistoryPage struct {
	Items      []AgentHistoryItem `json:"items"`
	NextCursor string             `json:"nextCursor"`
}

type ModelOption struct {
	ProviderID               string   `json:"providerId"`
	ModelID                  string   `json:"modelId"`
	DefaultProviderID        string   `json:"defaultProviderId"`
	DefaultModelID           string   `json:"defaultModelId"`
	Label                    string   `json:"label"`
	ProviderName             string   `json:"providerName"`
	ReasoningLevels          []string `json:"reasoningLevels"`
	DefaultReasoningLevel    string   `json:"defaultReasoningLevel"`
	AssignedAgentDefinitions []string `json:"assignedAgentDefinitions"`
}

// ProviderConfigOption 携带可编辑的 Provider 元数据和密钥状态。
type ProviderConfigOption struct {
	ID             string `json:"id"`
	ProviderName   string `json:"providerName"`
	BaseURL        string `json:"baseURL"`
	ProxyURL       string `json:"proxyURL"`
	DefaultModelID string `json:"defaultModelId"`
	HasAPIKey      bool   `json:"hasAPIKey"`
}

type GroupConfigOption struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// ModelConfigOption 携带设置界面编辑模型所需的全部字段。
type ModelConfigOption struct {
	ProviderID            string                       `json:"providerId"`
	ModelID               string                       `json:"modelId"`
	Label                 string                       `json:"label"`
	GroupID               string                       `json:"groupId"`
	APIFormat             string                       `json:"apiFormat"`
	ContextWindow         int                          `json:"contextWindow"`
	MaxOutputTokens       int                          `json:"maxOutputTokens"`
	ReasoningLevels       []string                     `json:"reasoningLevels"`
	DefaultReasoningLevel string                       `json:"defaultReasoningLevel"`
	Thinking              modelcontracts.ModelThinking `json:"thinking,omitzero"`
}

// ModelConfigDocument 表示可编辑的模型注册表文档。
type ModelConfigDocument struct {
	Groups            []GroupConfigOption    `json:"groups"`
	DefaultProviderID string                 `json:"defaultProviderId"`
	Providers         []ProviderConfigOption `json:"providers"`
	Models            []ModelConfigOption    `json:"models"`
}

type SaveModelConfigRequest struct {
	Groups            []GroupConfigOption    `json:"groups"`
	DefaultProviderID string                 `json:"defaultProviderId"`
	Providers         []ProviderConfigOption `json:"providers"`
	Models            []ModelConfigOption    `json:"models"`
}

// SaveModelConfigResponse 通过 validationError 报告用户可修正的配置问题。
// 这些问题不属于调用失败，不映射为 binding 错误码，以便界面展示具体修正信息。
type SaveModelConfigResponse struct {
	Saved           bool   `json:"saved"`
	ValidationError string `json:"validationError,omitempty"`
}

type SendInputRequest struct {
	SessionID      string `json:"sessionId"`
	AgentID        string `json:"agentId"`
	RequestID      string `json:"requestId"`
	Content        string `json:"content"`
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}

type SendInputResponse struct {
	TaskID          string `json:"taskId"`
	ExistingRequest bool   `json:"existingRequest"`
	ActivationError string `json:"activationError,omitempty"`
}

type PauseAgentRequest struct {
	CommandID    string `json:"commandId"`
	AgentID      string `json:"agentId"`
	TargetTaskID string `json:"targetTaskId"`
}

type CancelTaskRequest = PauseAgentRequest

type AgentControlResponse struct {
	CommandID         string `json:"commandId"`
	AgentID           string `json:"agentId"`
	TargetTaskID      string `json:"targetTaskId,omitempty"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	ExistingCommand   bool   `json:"existingCommand"`
	CancellationError string `json:"cancellationError,omitempty"`
}

type QueueTaskRequest struct {
	TaskID         string `json:"taskId"`
	RequestID      string `json:"requestId"`
	AgentID        string `json:"agentId"`
	Prompt         string `json:"prompt"`
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}

type QueueTaskResponse struct {
	TaskID       string `json:"taskId"`
	Status       string `json:"status"`
	ExistingTask bool   `json:"existingTask"`
}
