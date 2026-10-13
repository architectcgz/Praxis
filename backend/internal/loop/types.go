package loop

import (
	"context"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	taskmodel "praxis/internal/core/task"
	toolcontracts "praxis/internal/tools/contracts"
)

// ToolExecutor 执行已经登记的调用；等待审批时不产生工具回执。
type ToolExecutor interface {
	Execute(context.Context, contracts.ToolCall, contracts.ToolInvocationContext) (toolcontracts.ToolExecutionResult, error)
}

type (
	ToolCall                   = contracts.ToolCall
	ToolDefinition             = contracts.ToolDefinition
	ModelContext               = appcontext.ModelContext
	ContextWindowExceededError = appcontext.ContextWindowExceededError
)

// TaskContextBuilder 为首次执行和恢复执行提供同一上下文构建入口。
type TaskContextBuilder interface {
	RebuildTaskContext(context.Context, taskmodel.Task) (appcontext.BuildResult, error)
}

// TaskProgress 提供已提交的 Task 快照及模型消耗记录；事务由实现方负责。
// 工具调用次数和回执字节由工具准入、消息写入负责，Loop 只读取累计结果。
type TaskProgress interface {
	// LoadTask 返回当前持久化快照；读取失败时停止执行，不用旧预算继续运行。
	LoadTask(context.Context, contracts.TaskID) (taskmodel.Task, error)
	// RecordModelRequest 在模型请求前推进序号和请求次数；失败时不得调用模型。
	RecordModelRequest(context.Context, contracts.TaskID, uint64) error
	// RecordModelOutput 保存一次模型响应的文本消耗，取消后也不丢失已用额度。
	RecordModelOutput(context.Context, contracts.TaskID, int) error
}
