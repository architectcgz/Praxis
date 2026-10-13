package loop

import (
	"context"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolcontracts "praxis/internal/tools/contracts"
)

// TurnExecutor 负责单次 Turn 的持久化生命周期；loop 只提供该 Turn 的业务执行函数。
// Execute 必须先记录 running，再执行 operation，最后记录经过取消规则映射的终态。
type TurnExecutor interface {
	Execute(
		context.Context,
		contracts.TurnExecutionContext,
		func(context.Context, contracts.TurnID) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error),
	) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error)
}


// ToolCallHandler 原子登记并执行 loop 产生的工具调用。
type ToolCallHandler interface {
	// RecordAssistant 原子保存 assistant 消息及其全部 requested 调用；失败时不得执行工具。
	RecordAssistant(
		context.Context,
		func(context.Context, sessionmodel.MessageData) (sessionmodel.MessageData, error),
		sessionmodel.MessageData,
		contracts.ToolInvocationContext,
	) error
	Invoke(context.Context, contracts.ToolCall, contracts.ToolInvocationContext) (toolcontracts.ToolResult, error)
}

type (
	ToolCall                   = contracts.ToolCall
	ToolDefinition             = contracts.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)
