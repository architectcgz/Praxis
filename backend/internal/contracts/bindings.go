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
	ID           string          `json:"id"`
	Goal         string          `json:"goal"`
	WorkspaceKey string          `json:"workspaceKey"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Groups       []GroupSnapshot `json:"groups"`
	Agents       []AgentSnapshot `json:"agents"`
}

type SessionSummary struct {
	ID           string    `json:"id"`
	Goal         string    `json:"goal"`
	WorkspaceKey string    `json:"workspaceKey"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type CreateProjectRequest struct {
	ProjectName string `json:"projectName"`
	Goal        string `json:"goal"`
}

type CreateProjectResponse struct {
	SessionID    string `json:"sessionId"`
	AgentID      string `json:"agentId"`
	Goal         string `json:"goal"`
	WorkspaceKey string `json:"workspaceKey"`
}

type CreateSessionRequest struct {
	WorkspaceKey string `json:"workspaceKey"`
	Goal         string `json:"goal"`
}

type CreateSessionResponse struct {
	SessionID    string `json:"sessionId"`
	AgentID      string `json:"agentId"`
	Goal         string `json:"goal"`
	WorkspaceKey string `json:"workspaceKey"`
}

type GroupSnapshot struct {
	ID             string `json:"id"`
	PrimaryAgentID string `json:"primaryAgentId"`
	MaxConcurrent  int    `json:"maxConcurrent"`
}

type AgentSnapshot struct {
	ID                string              `json:"id"`
	SessionID         string              `json:"sessionId"`
	GroupID           string              `json:"groupId"`
	Profile           string              `json:"profile"`
	State             string              `json:"state"`
	CurrentExecution  string              `json:"currentExecutionId"`
	ExecutionIDs      []string            `json:"executionIds"`
	Executions        []ExecutionSnapshot `json:"executions"`
	WaitConditionIDs  []string            `json:"waitConditionIds"`
	DeliveryIDs       []string            `json:"deliveryIds"`
	ControlRequestIDs []string            `json:"controlRequestIds"`
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

// ReasoningOption is the model-declared reasoning capability presented by the
// frontend. It never contains provider credentials or endpoint details.
type ReasoningOption struct {
	Supported bool     `json:"supported"`
	Levels    []string `json:"levels"`
	Default   string   `json:"default"`
}

type ModelOption struct {
	ID              string          `json:"id"`
	Label           string          `json:"label"`
	ProviderLabel   string          `json:"providerLabel"`
	Reasoning       ReasoningOption `json:"reasoning"`
	DefaultProfiles []string        `json:"defaultProfiles"`
}

type SendInputRequest struct {
	AgentID      string `json:"agentId"`
	RequestID    string `json:"requestId"`
	Content      string `json:"content"`
	ModelID      string `json:"modelId"`
	Reasoning    string `json:"reasoning"`
	SandboxMode  string `json:"sandboxMode"`
	ApprovalMode string `json:"approvalMode"`
	Revision     string `json:"revision"`
}

type SendInputResponse struct {
	ExecutionID     string `json:"executionId"`
	ExistingRequest bool   `json:"existingRequest"`
	ActivationError string `json:"activationError,omitempty"`
}

type ResumeRequest struct {
	AgentID      string `json:"agentId"`
	RequestID    string `json:"requestId"`
	Content      string `json:"content"`
	SandboxMode  string `json:"sandboxMode"`
	ApprovalMode string `json:"approvalMode"`
	Revision     string `json:"revision"`
}

type ControlRequest struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	Kind    string `json:"kind"`
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
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	Prompt       string `json:"prompt"`
	SandboxMode  string `json:"sandboxMode"`
	ApprovalMode string `json:"approvalMode"`
	Revision     string `json:"revision"`
}

type QueueWorkResponse struct {
	WorkID          string `json:"workId"`
	ExecutionID     string `json:"executionId,omitempty"`
	Status          string `json:"status"`
	ExistingWork    bool   `json:"existingWork"`
	ActivationError string `json:"activationError,omitempty"`
}
