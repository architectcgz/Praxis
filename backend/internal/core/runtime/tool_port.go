package runtime

import (
	"context"
	"encoding/json"

	"praxis/internal/core/domain"
)

// ToolCall is a provider-neutral request to execute one tool.
type ToolCall struct {
	ID                  string
	Name                domain.ToolName
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
	Grant          domain.CapabilityGrant
	Execution      domain.RuntimeExecutionSnapshot
	LeaseReference string
}

// ToolExecutionResult is the bounded result returned by a tool executor.
type ToolExecutionResult struct {
	Content    string
	Output     string
	ErrorClass string
	SideEffect bool
}

// ToolExecutor owns the concrete side effect behind a runtime tool call.
// The core owns this contract so tool adapters depend on the core rather than
// on a sibling adapter package.
type ToolExecutor interface {
	Execute(context.Context, ToolCall, ToolExecutionContext) (ToolExecutionResult, error)
}
