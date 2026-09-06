package agentruntime

import (
	domainfoundation "praxis/internal/core/domain/foundation"

	coreruntime "praxis/internal/core/runtime"
)

type (
	ModelStream           = coreruntime.ModelStream
	ModelRequest          = coreruntime.ModelRequest
	ModelStreamEvent      = coreruntime.ModelStreamEvent
	ModelStreamEventKind  = coreruntime.ModelStreamEventKind
	ToolInvoker           = coreruntime.ToolInvoker
	ToolCall              = coreruntime.ToolCall
	ToolDefinition        = coreruntime.ToolDefinition
	ToolInvocationContext = coreruntime.ToolInvocationContext
	ToolResult            = coreruntime.ToolResult
	TurnSnapshot          = coreruntime.TurnSnapshot
	TurnMessage           = coreruntime.TurnMessage
	TurnMessageRole       = coreruntime.TurnMessageRole
	TurnContentBlock      = coreruntime.TurnContentBlock
	TurnContentBlockKind  = coreruntime.TurnContentBlockKind
)

const (
	StreamTextDelta       = coreruntime.StreamTextDelta
	StreamToolCall        = coreruntime.StreamToolCall
	StreamComplete        = coreruntime.StreamComplete
	StreamError           = coreruntime.StreamError
	TurnRoleUser          = coreruntime.TurnRoleUser
	TurnRoleAssistant     = coreruntime.TurnRoleAssistant
	TurnContentText       = coreruntime.TurnContentText
	TurnContentThinking   = coreruntime.TurnContentThinking
	TurnContentToolUse    = coreruntime.TurnContentToolUse
	TurnContentToolResult = coreruntime.TurnContentToolResult
)

type AgentOutputEventKind string

const (
	AgentOutputTextDelta AgentOutputEventKind = "text_delta"
	AgentOutputSettled   AgentOutputEventKind = "settled"
)

type AgentOutputEvent struct {
	Kind        AgentOutputEventKind              `json:"kind"`
	AgentID     domainfoundation.AgentID          `json:"agentId"`
	ExecutionID domainfoundation.AgentExecutionID `json:"executionId"`
	Text        string                            `json:"text"`
}

type AgentOutputObserver func(AgentOutputEvent)
