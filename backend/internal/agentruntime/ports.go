package agentruntime

import (
	"context"

	"praxis/internal/core/domain"
)

// ModelStreamPort adapts a provider into the runtime's streaming contract.
type ModelStreamPort interface {
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}

// ToolExecutor owns the concrete side effect behind a runtime tool call.
type ToolExecutor interface {
	Execute(context.Context, ToolCall, ToolExecutionContext) (ToolExecutionResult, error)
}

// AgentSessionStore owns durable append and safe context projection for one agent session.
type AgentSessionStore interface {
	Append(context.Context, []AgentSessionEntry) error
	ReadContext(context.Context, string) (AgentSessionContext, error)
	FindArtifact(context.Context, string, string) (*SessionEntryRef, error)
}

// SessionFlusher is an optional durability boundary for stores backed by buffered writers.
type SessionFlusher interface {
	Flush(context.Context) error
}

// RuntimeFactory creates a runtime from a fully approved snapshot.
type RuntimeFactory interface {
	New(context.Context, RuntimeConfig) (*Runtime, error)
}

// Factory is the default runtime factory used by composition wiring.
type Factory struct{}

// New creates a runtime from a fully approved snapshot.
func (Factory) New(_ context.Context, config RuntimeConfig) (*Runtime, error) {
	return New(config)
}

// ValidateIDs checks that a run belongs to the runtime's immutable thread boundary.
func ValidateIDs(run domain.AgentRun, taskSessionID domain.TaskSessionID, threadID domain.AgentThreadID) error {
	if run.AgentThreadID != threadID {
		return &RuntimeError{Code: ErrorContract, Message: "run does not belong to runtime thread"}
	}
	if taskSessionID == "" || threadID == "" {
		return &RuntimeError{Code: ErrorContract, Message: "runtime identity is incomplete"}
	}
	return nil
}
