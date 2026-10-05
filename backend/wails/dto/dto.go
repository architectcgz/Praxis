package dto

import (
	"encoding/json"
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
	ID                     string         `json:"id"`
	Name                   string         `json:"name"`
	SessionID              string         `json:"sessionId"`
	DefinitionID           string         `json:"definitionId"`
	SecurityPolicyRevision uint64         `json:"securityPolicyRevision"`
	Profile                string         `json:"profile"`
	State                  string         `json:"state"`
	CurrentTurn            string         `json:"currentTurnId"`
	TurnIDs                []string       `json:"turnIds"`
	Turns                  []TurnSnapshot `json:"turns"`
	WaitConditionIDs       []string       `json:"waitConditionIds"`
	ControlCommandIDs      []string       `json:"controlCommandIds"`
}

// TurnSnapshot 返回持久化生命周期与安全的失败码，不暴露凭据或原始错误。
type TurnSnapshot struct {
	ID             string    `json:"id"`
	Reason         string    `json:"reason"`
	Status         string    `json:"status"`
	Outcome        string    `json:"outcome,omitempty"`
	FailureCode    string    `json:"failureCode,omitempty"`
	FailureMessage string    `json:"failureMessage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	SettledAt      time.Time `json:"settledAt,omitempty"`
}

type AgentMessage struct {
	ID         string              `json:"id"`
	Sequence   uint64              `json:"sequence"`
	At         time.Time           `json:"at"`
	TurnID     string              `json:"turnId"`
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
	Turn     *TurnSnapshot `json:"turn,omitempty"`
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

// ProviderConfigOption carries editable provider metadata and secret status.
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

// ModelConfigOption carries every field the settings UI needs to edit a model.
type ModelConfigOption struct {
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	Label                 string   `json:"label"`
	GroupID               string   `json:"groupId"`
	APIFormat             string   `json:"apiFormat"`
	ContextWindow         int      `json:"contextWindow"`
	MaxOutputTokens       int      `json:"maxOutputTokens"`
	ReasoningLevels       []string `json:"reasoningLevels"`
	DefaultReasoningLevel string   `json:"defaultReasoningLevel"`
}

// ModelConfigDocument is one editable model registry document.
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

// SaveModelConfigResponse reports user-fixable configuration problems through
// validationError. Those are not call failures, so they bypass the opaque
// binding error codes and stay actionable in the UI.
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
	TurnID          string `json:"turnId"`
	ExistingRequest bool   `json:"existingRequest"`
	ActivationError string `json:"activationError,omitempty"`
}

type ResumeRequest struct {
	AgentID   string `json:"agentId"`
	RequestID string `json:"requestId"`
	Content   string `json:"content"`
}

type PauseAgentRequest struct {
	CommandID string `json:"commandId"`
	AgentID   string `json:"agentId"`
}

type CloseAgentRequest = PauseAgentRequest

type AgentControlResponse struct {
	CommandID         string `json:"commandId"`
	AgentID           string `json:"agentId"`
	TargetTurnID      string `json:"targetTurnId,omitempty"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	ExistingCommand   bool   `json:"existingCommand"`
	CancellationError string `json:"cancellationError,omitempty"`
}

type QueueWorkRequest struct {
	ID        string `json:"id"`
	RequestID string `json:"requestId"`
	AgentID   string `json:"agentId"`
	Prompt    string `json:"prompt"`
}

type QueueWorkResponse struct {
	WorkID          string `json:"workId"`
	TurnID          string `json:"turnId,omitempty"`
	Status          string `json:"status"`
	ExistingWork    bool   `json:"existingWork"`
	ActivationError string `json:"activationError,omitempty"`
}
