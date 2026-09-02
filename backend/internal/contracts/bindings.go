package contracts

import "time"

// StartupIssue is a safe diagnostic that remains available when startup could
// not open the application. Configuration failures identify the file and its
// first validation error without exposing secret values.
type StartupIssue struct {
	Code    ErrorCode `json:"code"`
	Path    string    `json:"path,omitempty"`
	Message string    `json:"message"`
}

// HealthSnapshot is the only binding state that is available while startup
// recovery has command admission closed or configuration failed to load.
type HealthSnapshot struct {
	Ready bool          `json:"ready"`
	Issue *StartupIssue `json:"issue,omitempty"`
}

type SessionSnapshot struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	WorkspaceID string          `json:"workspaceId"`
	Goal        string          `json:"goal"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	Agents      []AgentSnapshot `json:"agents"`
}

type SessionSummary struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	WorkspaceID string    `json:"workspaceId"`
	Goal        string    `json:"goal"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreateProjectRequest struct {
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
	ProjectID   string `json:"projectId"`
	WorkspaceID string `json:"workspaceId"`
	Goal        string `json:"goal"`
	RequestID   string `json:"requestId"`
}

type CreateSessionResponse struct {
	SessionID   string `json:"sessionId"`
	ProjectID   string `json:"projectId"`
	WorkspaceID string `json:"workspaceId"`
	AgentID     string `json:"agentId"`
	Goal        string `json:"goal"`
}

type AgentSnapshot struct {
	ID                     string              `json:"id"`
	SessionID              string              `json:"sessionId"`
	SecurityPolicyRevision uint64              `json:"securityPolicyRevision"`
	Profile                string              `json:"profile"`
	State                  string              `json:"state"`
	CurrentExecution       string              `json:"currentExecutionId"`
	ExecutionIDs           []string            `json:"executionIds"`
	Executions             []ExecutionSnapshot `json:"executions"`
	WaitConditionIDs       []string            `json:"waitConditionIds"`
	DeliveryIDs            []string            `json:"deliveryIds"`
	ControlRequestIDs      []string            `json:"controlRequestIds"`
}

// ExecutionSnapshot exposes the durable lifecycle and safe failure code of
// an Agent execution without exposing provider credentials or raw errors.
type ExecutionSnapshot struct {
	ID          string    `json:"id"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	Outcome     string    `json:"outcome,omitempty"`
	FailureCode string    `json:"failureCode,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	SettledAt   time.Time `json:"settledAt,omitempty"`
}

type AgentMessage struct {
	Sequence    uint64    `json:"sequence"`
	At          time.Time `json:"at"`
	ExecutionID string    `json:"executionId"`
	Role        string    `json:"role"`
	Content     string    `json:"content"`
}

type AgentHistoryItem struct {
	Kind      string             `json:"kind"`
	Sequence  uint64             `json:"sequence,omitempty"`
	At        time.Time          `json:"at"`
	Message   *AgentMessage      `json:"message,omitempty"`
	Execution *ExecutionSnapshot `json:"execution,omitempty"`
}

type EventSnapshot struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	OccurredAt  time.Time         `json:"occurredAt"`
	SessionID   string            `json:"sessionId,omitempty"`
	AgentID     string            `json:"agentId,omitempty"`
	ExecutionID string            `json:"executionId,omitempty"`
	Payload     map[string]string `json:"payload,omitempty"`
}

// ReasoningOption is the model-declared reasoning capability presented by the
// frontend. It never contains provider credentials or endpoint details.
type ReasoningOption struct {
	Supported bool     `json:"supported"`
	Levels    []string `json:"levels"`
	Default   string   `json:"default"`
}

type ModelOption struct {
	ProviderID      string          `json:"providerId"`
	ModelID         string          `json:"modelId"`
	Label           string          `json:"label"`
	ProviderName    string          `json:"providerName"`
	Reasoning       ReasoningOption `json:"reasoning"`
	DefaultProfiles []string        `json:"defaultProfiles"`
}

// ProviderConfigOption carries editable provider metadata and secret status.
type ProviderConfigOption struct {
	ID           string `json:"id"`
	ProviderName string `json:"providerName"`
	BaseURL      string `json:"baseURL"`
	ProxyURL     string `json:"proxyURL"`
	HasAPIKey    bool   `json:"hasAPIKey"`
}

// ModelConfigOption carries every field the settings UI needs to edit a model.
type ModelConfigOption struct {
	ProviderID      string          `json:"providerId"`
	ModelID         string          `json:"modelId"`
	Label           string          `json:"label"`
	APIFormat       string          `json:"apiFormat"`
	ContextWindow   int             `json:"contextWindow"`
	MaxOutputTokens int             `json:"maxOutputTokens"`
	Reasoning       ReasoningOption `json:"reasoning"`
}

// ModelConfigDocument is one editable configuration document. The UI submits it
// whole so per-entry edits cannot leave dangling references, such as deleting a
// model that a profile still binds.
type ModelConfigDocument struct {
	Providers []ProviderConfigOption    `json:"providers"`
	Models    []ModelConfigOption       `json:"models"`
	Profiles  map[string]ModelReference `json:"profiles"`
	// ProfileNames is the profile allowlist the backend accepts, so the UI renders
	// exactly the bindings that will validate.
	ProfileNames []string `json:"profileNames"`
}

type SaveModelConfigRequest struct {
	Providers []ProviderConfigOption    `json:"providers"`
	Models    []ModelConfigOption       `json:"models"`
	Profiles  map[string]ModelReference `json:"profiles"`
}

// SaveModelConfigResponse reports user-fixable configuration problems through
// validationError. Those are not call failures, so they bypass the opaque
// binding error codes and stay actionable in the UI.
type SaveModelConfigResponse struct {
	Saved           bool   `json:"saved"`
	ValidationError string `json:"validationError,omitempty"`
}

type ModelReference struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

type SendInputRequest struct {
	SessionID  string `json:"sessionId"`
	AgentID    string `json:"agentId"`
	RequestID  string `json:"requestId"`
	Content    string `json:"content"`
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Reasoning  string `json:"reasoning"`
}

type SendInputResponse struct {
	ExecutionID     string `json:"executionId"`
	ExistingRequest bool   `json:"existingRequest"`
	ActivationError string `json:"activationError,omitempty"`
}

type ResumeRequest struct {
	AgentID   string `json:"agentId"`
	RequestID string `json:"requestId"`
	Content   string `json:"content"`
}

type ControlRequest struct {
	RequestID string `json:"requestId"`
	AgentID   string `json:"agentId"`
	Kind      string `json:"kind"`
}

type ControlResponse struct {
	RequestID         string `json:"requestId"`
	AgentID           string `json:"agentId"`
	TargetExecutionID string `json:"targetExecutionId,omitempty"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	ExistingRequest   bool   `json:"existingRequest"`
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
	ExecutionID     string `json:"executionId,omitempty"`
	Status          string `json:"status"`
	ExistingWork    bool   `json:"existingWork"`
	ActivationError string `json:"activationError,omitempty"`
}
