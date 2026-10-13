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
type TaskRepository interface {
	Get(context.Context, contracts.TaskID) (taskmodel.Task, error)
	Save(context.Context, taskmodel.Task) error
}
