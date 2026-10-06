package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// TaskLifecycle 提供回合开始和结束接口；End 返回已持久化且经过控制命令仲裁的终态。
type TaskLifecycle interface {
	Start(context.Context, contracts.TaskID) error
	End(
		context.Context,
		contracts.TaskID,
		taskmodel.TaskOutcome,
		contracts.TaskFailureCode,
		string,
	) (taskmodel.Task, error)
}
