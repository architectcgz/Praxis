package execution

import (
	"context"
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/loop"
)

// TaskOperations 保留 Task 准备、恢复和结算的事务边界。
type TaskOperations interface {
	PrepareQueuedTask(context.Context, taskmodel.Task) (taskmodel.Task, bool, error)
	ReclaimTask(context.Context, taskmodel.Task) (taskmodel.Task, error)
	ConfirmRunning(context.Context, contracts.TaskID) error
	Settle(context.Context, contracts.TaskID, taskmodel.TaskOutcome, contracts.TaskFailureCode, string) (taskmodel.Task, error)
}

// TaskQueue 只查询指定 Agent 的队首，准备成功后才推进持久化状态。
type TaskQueue interface {
	Next(context.Context, contracts.AgentID) (taskmodel.Task, bool, error)
}

// MessageRecorderResolver 为一次执行解析其所属的消息流。
type MessageRecorderResolver func(context.Context, contracts.SessionID, contracts.AgentID) (loop.MessageRecorder, error)

// taskResult 暂存待提交的结算结果，提交失败后只重试结算，不重复执行 Loop。
type taskResult struct {
	outcome taskmodel.TaskOutcome
	code    contracts.TaskFailureCode
	message string
}
