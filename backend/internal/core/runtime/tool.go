package runtime

import (
	"context"
	"encoding/json"

	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
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
}

// ToolInvoker submits a model tool call through the durable application boundary.
type ToolInvoker interface {
	Invoke(context.Context, ToolCall, ToolInvocationContext) (ToolResult, error)
}
