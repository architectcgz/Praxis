package execution

import (
	"context"

	appcontext "praxis/internal/context"
	"praxis/internal/identity"
	"praxis/internal/message"
	"praxis/internal/task"
	taskservice "praxis/internal/task/service"
)

// TaskOperations 是 Coordinator 需要的 Task 用例能力；具体实现属于 task/service。
type TaskOperations interface {
	task.Store
	SendInput(context.Context, taskservice.SendInput) (taskservice.SendInputResult, error)
	LoadContext(context.Context, task.Task) (appcontext.Content, []message.Message, error)
	ConfirmRunning(context.Context, identity.TaskID) error
	Settle(context.Context, identity.TaskID, task.TaskOutcome, task.TaskFailureCode, string) (task.Task, error)
	ReclaimTask(context.Context, task.Task) (task.Task, error)
	ReconcileToolResults(context.Context) error
	Abort(context.Context, identity.TaskID) error
	ReleaseExecution(identity.AgentID, identity.TaskID)
}

// TaskQueue 是 Coordinator 在 Task 结束后领取下一项预约任务所需的能力。
type TaskQueue interface {
	EnqueueTask(context.Context, taskservice.EnqueueInput) (taskservice.EnqueueResult, error)
	Next(context.Context, identity.AgentID) (task.Task, bool, error)
	PrepareQueuedTask(context.Context, task.Task) (task.Task, bool, error)
}

type taskResult struct {
	outcome task.TaskOutcome
	code    task.TaskFailureCode
	message string
}
