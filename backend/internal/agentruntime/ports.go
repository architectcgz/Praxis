package agentruntime

import (
	"context"
	"time"
)

// ModelStreamPort adapts a provider into the runtime's streaming contract.
type ModelStreamPort interface {
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}

// ToolExecutor owns the concrete side effect behind a runtime tool call.
type ToolExecutor interface {
	Execute(context.Context, ToolCall, ToolExecutionContext) (ToolExecutionResult, error)
}

// AgentSessionStore owns session initialization, durable event append, and safe
// context projection for one agent session.
type AgentSessionStore interface {
	Initialize(context.Context, SessionManifest, time.Time) (SessionAppendResult, error)
	Append(context.Context, SessionEvent) (SessionAppendResult, error)
	ReadContext(context.Context, string) (SessionContext, error)
}

// SessionFlusher is an optional durability boundary for stores backed by buffered writers.
type SessionFlusher interface {
	Flush(context.Context) error
}
