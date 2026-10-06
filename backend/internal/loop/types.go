package loop

import (
	appcontext "praxis/internal/core/context"
	toolcontracts "praxis/internal/tools/contracts"

	"praxis/internal/agent_runtime"
)

type (
	ModelRequest               = agentruntime.ModelRequest
	ModelStreamEvent           = agentruntime.ModelStreamEvent
	ToolCallHandler            = agentruntime.ToolCallHandler
	ToolCatalog                = agentruntime.ToolCatalog
	TurnRecorder               = agentruntime.TurnRecorder
	TurnParams                 = agentruntime.TurnParams
	ToolCall                   = toolcontracts.ToolCall
	ToolDefinition             = toolcontracts.ToolDefinition
	ToolInvocationMetadata     = agentruntime.ToolInvocationMetadata
	ModelContext               = appcontext.ModelContext
	TaskModel                  = agentruntime.TaskModel
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

const (
	StreamTextDelta     = agentruntime.StreamTextDelta
	StreamThinkingDelta = agentruntime.StreamThinkingDelta
	StreamToolCall      = agentruntime.StreamToolCall
	StreamUsage         = agentruntime.StreamUsage
	StreamComplete      = agentruntime.StreamComplete
	StreamError         = agentruntime.StreamError
)
