package agentruntime

import (
	domainfoundation "praxis/internal/domain/foundation"

	runtimecontract "praxis/internal/runtime"
)

type (
	ModelStream           = runtimecontract.ModelStream
	ModelRequest          = runtimecontract.ModelRequest
	ModelStreamEvent      = runtimecontract.ModelStreamEvent
	ModelStreamEventKind  = runtimecontract.ModelStreamEventKind
	ToolInvoker           = runtimecontract.ToolInvoker
	ToolCatalog           = runtimecontract.ToolCatalog
	ToolCall              = runtimecontract.ToolCall
	AuthorizedToolCall    = runtimecontract.AuthorizedToolCall
	ToolDefinition        = runtimecontract.ToolDefinition
	ToolInvocationContext = runtimecontract.ToolInvocationContext
	ToolResult            = runtimecontract.ToolResult
	TurnSnapshot          = runtimecontract.TurnSnapshot
	TurnMessage           = runtimecontract.TurnMessage
	TurnMessageRole       = runtimecontract.TurnMessageRole
	TurnContentBlock      = runtimecontract.TurnContentBlock
	TurnContentBlockKind  = runtimecontract.TurnContentBlockKind
)

const (
	StreamTextDelta       = runtimecontract.StreamTextDelta
	StreamToolCall        = runtimecontract.StreamToolCall
	StreamComplete        = runtimecontract.StreamComplete
	StreamError           = runtimecontract.StreamError
	TurnRoleUser          = runtimecontract.TurnRoleUser
	TurnRoleAssistant     = runtimecontract.TurnRoleAssistant
	TurnRoleTool          = runtimecontract.TurnRoleTool
	TurnContentText       = runtimecontract.TurnContentText
	TurnContentThinking   = runtimecontract.TurnContentThinking
	TurnContentToolUse    = runtimecontract.TurnContentToolUse
	TurnContentToolResult = runtimecontract.TurnContentToolResult
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
