package runtime

import (
	"context"
	"encoding/json"

	domaincontext "praxis/internal/domain/context"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainmodel "praxis/internal/domain/model"
	domainsecurity "praxis/internal/domain/security"
)

// TurnMessageRole identifies the participant that produced a turn message.
type TurnMessageRole string

const (
	TurnRoleUser      TurnMessageRole = "user"
	TurnRoleAssistant TurnMessageRole = "assistant"
	TurnRoleTool      TurnMessageRole = "tool"
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
	Name        domainsecurity.ToolName
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
	Snapshot ExecutionTurnSnapshot
}

// ExecutionTurnSnapshot is the immutable input for one execution turn model request.
type ExecutionTurnSnapshot struct {
	ExecutionID          domainfoundation.AgentExecutionID
	SessionReference     string
	Messages             []TurnMessage
	ContextManifest      domaincontext.ContextManifest
	ContextSelection     domainexecution.ContextSelection
	SystemPrompt         string
	SystemPromptHash     string
	ArtifactTemplateHash string
	Model                domainmodel.ModelSelection
	MaxOutputTokens      int
	Tools                []ToolDefinition
	GrantID              domainfoundation.CapabilityGrantID
	Execution            domainexecution.RuntimeExecutionSnapshot
	TurnNumber           int
}

// ModelStream is the provider-neutral streaming contract owned by the core.
// Provider adapters implement it without exposing protocol details inward.
type ModelStream interface {
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
