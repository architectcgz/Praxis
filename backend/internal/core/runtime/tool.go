package runtime

import (
	"context"
	"encoding/json"

	domainexecution "praxis/internal/core/domain/execution"
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

// ToolExecutionContext is the immutable authorization snapshot passed to a tool executor.
type ToolExecutionContext struct {
	Grant          domainsecurity.CapabilityGrant
	Execution      domainexecution.RuntimeExecutionSnapshot
	LeaseReference string
}

// ToolExecutionResult is the bounded result returned by a tool executor.
type ToolExecutionResult struct {
	Content    string
	Output     string
	ErrorClass string
	SideEffect bool
}

// ToolRunner owns the concrete side effect behind a runtime tool call.
// The core owns this contract so adapters depend on the core rather than on a
// sibling implementation package.
type ToolRunner interface {
	Execute(context.Context, ToolCall, ToolExecutionContext) (ToolExecutionResult, error)
}
