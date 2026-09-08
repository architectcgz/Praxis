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

// AuthorizedToolCall is the normalized execution input used by a tool executor.
//
// The application service creates this value after validating the provider
// request, then passes it to the executor only after the immutable execution
// grant has admitted the invocation. The executor receives this type instead
// of ToolCall so it cannot accidentally execute the raw, unnormalized provider
// input.
type AuthorizedToolCall struct {
	// Name identifies the registered tool to execute.
	Name                domainsecurity.ToolName
	// NormalizedArguments contains the tool-specific arguments after validation
	// and canonicalization. It remains JSON because the runtime layer is
	// provider-neutral and must support tools with different argument types.
	NormalizedArguments json.RawMessage
	// Path is the canonical filesystem path extracted from the arguments, when
	// the tool operates on a filesystem location.
	Path                string
	// ReadScopes contains the filesystem locations granted to this execution.
	ReadScopes          []string
	// RequiresWrite indicates that the tool would need write capability.
	RequiresWrite       bool
	// RequiresNetwork indicates that the tool would need network capability.
	RequiresNetwork     bool
}

// ToolCatalog owns model-visible schemas and deterministic argument normalization.
type ToolCatalog interface {
	Definition(domainsecurity.ToolName) (ToolDefinition, bool)
	// Normalize validates provider-supplied arguments and converts them into the
	// canonical form consumed by the authorization and execution stages.
	Normalize(ToolCall, ToolInvocationContext) (AuthorizedToolCall, error)
}

// ToolExecutor performs only calls that have passed durable application
// admission. Implementations should treat AuthorizedToolCall as a bounded,
// already-normalized execution input and still enforce tool-local invariants.
type ToolExecutor interface {
	Execute(context.Context, AuthorizedToolCall) (ToolResult, error)
}

// ToolInvoker submits a model tool call through the durable application boundary.
type ToolInvoker interface {
	Invoke(context.Context, ToolCall, ToolInvocationContext) (ToolResult, error)
}
