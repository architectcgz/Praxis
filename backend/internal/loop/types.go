package loop

import (
	"context"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	turnmodel "praxis/internal/core/turn"
	toolcontracts "praxis/internal/tools/contracts"
)

// TurnRecorder 持久化 loop 的迭代生命周期，实现方必须保证重复调用幂等。
type TurnRecorder interface {
	// RecordStart 按 Task 归属与序号持久化 running 记录；重复调用返回已有记录。
	RecordStart(context.Context, contracts.TurnExecutionContext) (turnmodel.Turn, error)
	// RecordEnd 按迭代 ID 持久化结果与失败信息；已结算时不重复写入。
	RecordEnd(context.Context, contracts.TurnID, turnmodel.TurnOutcome, contracts.TaskFailureCode, string) error
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
