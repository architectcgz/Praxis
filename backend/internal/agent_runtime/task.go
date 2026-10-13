package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// TaskPreparer 在 runtime 空闲时冻结待执行 Task 的输入，并原子推进至 Starting。
// 没有可执行 Task 时返回 false；构建失败保持原有 Pending 状态。
type TaskPreparer interface {
	PrepareQueuedTask(context.Context, taskmodel.Task) (taskmodel.Task, bool, error)
	ReclaimTask(context.Context, taskmodel.Task) (taskmodel.Task, error)
}

// TaskTransitions 确认执行开始并原子结算；Settle 返回控制命令仲裁后的持久化终态。
type TaskTransitions interface {
	ConfirmRunning(context.Context, contracts.TaskID) error
	Settle(context.Context, contracts.TaskID, taskmodel.TaskOutcome, contracts.TaskFailureCode, string) (taskmodel.Task, error)
}

// TaskQueue 保存等待 runtime 空闲后处理的输入；Next 只查询，不构建 Task 或 Context。
type TaskQueue interface {
	Next(context.Context) (taskmodel.Task, bool, error)
}
