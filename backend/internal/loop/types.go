package loop

import (
	appcontext "praxis/internal/context"
	toolcontracts "praxis/internal/tools/contracts"

	runtimecontract "praxis/internal/runtime"
)

type (
	ModelRequest               = runtimecontract.ModelRequest
	ModelStreamEvent           = runtimecontract.ModelStreamEvent
	ToolCallHandler            = runtimecontract.ToolCallHandler
	ToolCatalog                = runtimecontract.ToolCatalog
	ToolCall                   = toolcontracts.ToolCall
	ToolDefinition             = toolcontracts.ToolDefinition
	ToolInvocationMetadata     = runtimecontract.ToolInvocationMetadata
	ExecutionContext           = appcontext.ExecutionContext
	ExecutionModel             = runtimecontract.ExecutionModel
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

const (
	StreamTextDelta     = runtimecontract.StreamTextDelta
	StreamThinkingDelta = runtimecontract.StreamThinkingDelta
	StreamToolCall      = runtimecontract.StreamToolCall
	StreamComplete      = runtimecontract.StreamComplete
	StreamError         = runtimecontract.StreamError
)
