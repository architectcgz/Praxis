package runtime

import (
	"context"
	"encoding/json"

	"praxis/internal/core/domain"
)

// TurnMessageRole identifies the participant that produced a turn message.
type TurnMessageRole string

const (
	TurnRoleUser      TurnMessageRole = "user"
	TurnRoleAssistant TurnMessageRole = "assistant"
)

// TurnContentBlockKind identifies content returned by or supplied to a model.
type TurnContentBlockKind string

const (
	TurnContentText       TurnContentBlockKind = "text"
	TurnContentThinking   TurnContentBlockKind = "thinking"
	TurnContentToolUse    TurnContentBlockKind = "tool_use"
	TurnContentToolResult TurnContentBlockKind = "tool_result"
)

// TurnContentBlock is the provider-neutral content used while constructing a turn.
// It deliberately has no persistence tags; the transcript adapter owns its
// durable wire shape.
type TurnContentBlock struct {
	Kind       TurnContentBlockKind
	Text       string
	Signature  string
	ToolCallID string
	ToolName   string
	Input      json.RawMessage
	IsError    bool
}

// TurnMessage is a complete user or assistant message used to build a turn snapshot.
type TurnMessage struct {
	Role    TurnMessageRole
	Content []TurnContentBlock
}

// ToolDefinition describes a tool exposed to the model after Grant filtering.
type ToolDefinition struct {
	Name        domain.ToolName
	Description string
	InputSchema json.RawMessage
}

// ModelStreamEventKind identifies an event emitted by a model provider adapter.
type ModelStreamEventKind string

const (
	StreamTextDelta ModelStreamEventKind = "text_delta"
	StreamToolCall  ModelStreamEventKind = "tool_call"
	StreamComplete  ModelStreamEventKind = "complete"
	StreamError     ModelStreamEventKind = "error"
)

// ModelStreamEvent is a provider-neutral event from one model request.
type ModelStreamEvent struct {
	Kind       ModelStreamEventKind
	Text       string
	ToolCall   ToolCall
	StopReason string
	Err        error
}

// ModelRequest is the request sent to a model provider. It contains no credential fields.
type ModelRequest struct {
	Snapshot TurnSnapshot
}

// TurnSnapshot is the immutable input for one model request.
type TurnSnapshot struct {
	RunID                domain.AgentRunID
	SessionReference     string
	Messages             []TurnMessage
	TaskPacket           domain.TaskPacket
	ContextManifest      domain.ContextManifest
	SystemPrompt         string
	SystemPromptHash     string
	ArtifactTemplateHash string
	Model                domain.ModelRef
	Tools                []ToolDefinition
	GrantID              domain.CapabilityGrantID
	Execution            domain.RuntimeExecutionSnapshot
	TurnNumber           int
}

// ModelStreamPort adapts a provider into the runtime's streaming contract.
// The core owns this contract so provider adapters depend on the core rather
// than on a sibling adapter package.
type ModelStreamPort interface {
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
