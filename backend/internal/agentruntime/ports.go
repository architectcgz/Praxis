package agentruntime

import (
	"context"
	"time"

	coreruntime "praxis/internal/core/runtime"
)

// The model and tool contracts are owned by core/runtime so that provider and
// tool adapters depend on the core instead of on this sibling adapter package.
// The aliases below keep the runtime implementation readable without creating a
// second definition of the same contract.
type (
	ModelStreamPort      = coreruntime.ModelStreamPort
	ModelRequest         = coreruntime.ModelRequest
	ModelStreamEvent     = coreruntime.ModelStreamEvent
	ModelStreamEventKind = coreruntime.ModelStreamEventKind

	ToolExecutor         = coreruntime.ToolExecutor
	ToolCall             = coreruntime.ToolCall
	ToolDefinition       = coreruntime.ToolDefinition
	ToolExecutionContext = coreruntime.ToolExecutionContext
	ToolExecutionResult  = coreruntime.ToolExecutionResult

	TurnSnapshot         = coreruntime.TurnSnapshot
	TurnMessage          = coreruntime.TurnMessage
	TurnMessageRole      = coreruntime.TurnMessageRole
	TurnContentBlock     = coreruntime.TurnContentBlock
	TurnContentBlockKind = coreruntime.TurnContentBlockKind
)

const (
	StreamTextDelta = coreruntime.StreamTextDelta
	StreamToolCall  = coreruntime.StreamToolCall
	StreamComplete  = coreruntime.StreamComplete
	StreamError     = coreruntime.StreamError

	TurnRoleUser      = coreruntime.TurnRoleUser
	TurnRoleAssistant = coreruntime.TurnRoleAssistant

	TurnContentText       = coreruntime.TurnContentText
	TurnContentThinking   = coreruntime.TurnContentThinking
	TurnContentToolUse    = coreruntime.TurnContentToolUse
	TurnContentToolResult = coreruntime.TurnContentToolResult
)

// AgentSessionStore owns session initialization, durable event append, and safe
// context projection for one agent session. It stays here because it is the
// runtime's own transcript-writing contract; the core-side cross-store receipt
// port is core/session.TranscriptReceiptStore.
type AgentSessionStore interface {
	Initialize(context.Context, SessionManifest, time.Time) (SessionAppendResult, error)
	Append(context.Context, SessionEvent) (SessionAppendResult, error)
	ReadContext(context.Context, string) (SessionContext, error)
}

// SessionFlusher is an optional durability boundary for stores backed by buffered writers.
type SessionFlusher interface {
	Flush(context.Context) error
}
