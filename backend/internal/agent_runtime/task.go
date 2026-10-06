package agentruntime

import (
	"context"

	taskmodel "praxis/internal/core/task"
)

// TaskBuilder 在 runtime 空闲时为待执行 Task 构建 Context，并原子推进至 Starting。
// 没有可执行 Task 时返回 false；构建失败保持原有 Pending 状态。
type TaskBuilder interface {
	BuildTask(context.Context, taskmodel.Task) (taskmodel.Task, bool, error)
}

// TaskQueue 保存等待 runtime 空闲后处理的输入；Next 只查询，不构建 Task 或 Context。
type TaskQueue interface {
	Next(context.Context) (taskmodel.Task, bool, error)
}
