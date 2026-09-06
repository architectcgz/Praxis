package runtime

import (
	"context"
	"encoding/json"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

// ToolCall is a provider-neutral request to execute one tool.
type ToolCall struct {
	ID                  string
	Name                domainsecurity.ToolName
	Input               json.RawMessage
	Arguments           json.RawMessage
	Path                string
	WorkingDirectory    string
	RequiresWrite       bool
	RequiresNetwork     bool
	CommandConfirmation string
}

// ToolInvocationContext is the immutable execution boundary passed to the tool use case.
type ToolInvocationContext struct {
	ExecutionID    domainfoundation.AgentExecutionID
	SessionID      domainfoundation.SessionID
	AgentID        domainfoundation.AgentID
	Grant          domainsecurity.CapabilityGrant
	Execution      domainexecution.RuntimeExecutionSnapshot
	LeaseReference string
}

// ToolResult is the bounded result returned to the agent runtime.
type ToolResult struct {
	Content    string
	Output     string
	ErrorClass string
	SideEffect bool
	Truncated  bool
}

// AuthorizedToolCall is the normalized execution input produced after the
// application service has checked the immutable execution grant.
type AuthorizedToolCall struct {
	Name                domainsecurity.ToolName
	NormalizedArguments json.RawMessage
	Path                string
	ReadScopes          []string
	RequiresWrite       bool
	RequiresNetwork     bool
}

// ToolCatalog owns model-visible schemas and deterministic argument normalization.
type ToolCatalog interface {
	Definition(domainsecurity.ToolName) (ToolDefinition, bool)
	Normalize(ToolCall, ToolInvocationContext) (AuthorizedToolCall, error)
}

// ToolExecutor performs only calls that have passed durable application admission.
type ToolExecutor interface {
	Execute(context.Context, AuthorizedToolCall) (ToolResult, error)
}

// ToolInvoker submits a model tool call through the durable application boundary.
type ToolInvoker interface {
	Invoke(context.Context, ToolCall, ToolInvocationContext) (ToolResult, error)
}
