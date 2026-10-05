package loop

import (
	appcontext "praxis/internal/core/context"
	toolcontracts "praxis/internal/tools/contracts"

	runtimecontract "praxis/internal/agent_runtime"
)

type (
	ModelRequest               = runtimecontract.ModelRequest
	ModelStreamEvent           = runtimecontract.ModelStreamEvent
	ToolCallHandler            = runtimecontract.ToolCallHandler
	ToolCatalog                = runtimecontract.ToolCatalog
	ToolCall                   = toolcontracts.ToolCall
	ToolDefinition             = toolcontracts.ToolDefinition
	ToolInvocationMetadata     = runtimecontract.ToolInvocationMetadata
	ModelContext               = appcontext.ModelContext
	TurnModel                  = runtimecontract.TurnModel
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

const (
	StreamTextDelta     = runtimecontract.StreamTextDelta
	StreamThinkingDelta = runtimecontract.StreamThinkingDelta
	StreamToolCall      = runtimecontract.StreamToolCall
	StreamUsage         = runtimecontract.StreamUsage
	StreamComplete      = runtimecontract.StreamComplete
	StreamError         = runtimecontract.StreamError
)
