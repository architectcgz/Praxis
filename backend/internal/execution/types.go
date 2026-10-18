package execution

import (
	"context"

	taskmodel "praxis/internal/agent"
	taskservice "praxis/internal/agent/input"
	appcontext "praxis/internal/context"
	"praxis/internal/identity"
	"praxis/internal/message"
	queueservice "praxis/internal/task/service"
)

// TaskOperations 是 Coordinator 需要的 Task 用例能力；具体实现属于 task/service。
type TaskOperations interface {
	taskmodel.ExecutionStore
	SendInput(context.Context, taskservice.SendInput) (taskservice.SendInputResult, error)
	LoadContext(context.Context, taskmodel.Execution) (appcontext.Content, []message.Message, error)
	ConfirmRunning(context.Context, identity.AgentExecutionID) error
	Settle(context.Context, identity.AgentExecutionID, taskmodel.ExecutionOutcome, taskmodel.ExecutionFailureCode, string) (taskmodel.Execution, error)
	ReclaimTask(context.Context, taskmodel.Execution) (taskmodel.Execution, error)
	ReconcileToolResults(context.Context) error
	Abort(context.Context, identity.AgentExecutionID) error
	ReleaseExecution(identity.AgentID, identity.AgentExecutionID)
	PrepareNext(context.Context, identity.AgentID) (taskmodel.Execution, bool, error)
}

// TaskQueue 是 Coordinator 在 Task 结束后领取下一项预约任务所需的能力。
type TaskQueue interface {
	EnqueueTask(context.Context, queueservice.EnqueueInput) (queueservice.EnqueueResult, error)
}

type taskResult struct {
	outcome taskmodel.ExecutionOutcome
	code    taskmodel.ExecutionFailureCode
	message string
}
