package loop

import (
	appcontext "praxis/internal/core/context"
	toolcontracts "praxis/internal/tools/contracts"

	"praxis/internal/agent_runtime"
)

type (
	ToolCallHandler            = agentruntime.ToolCallHandler
	TurnRecorder               = agentruntime.TurnRecorder
	TurnParams                 = agentruntime.TurnParams
	ToolCall                   = toolcontracts.ToolCall
	ToolDefinition             = toolcontracts.ToolDefinition
	ToolInvocationMetadata     = agentruntime.ToolInvocationMetadata
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)
