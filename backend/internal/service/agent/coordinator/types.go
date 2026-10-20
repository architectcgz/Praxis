package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"praxis/internal/loop"
	executionservice "praxis/internal/service/agent/execution"
	queueservice "praxis/internal/service/task"
)

// ExecutionOperations 是 Coordinator 需要的 Execution 用例能力，具体实现属于 agent/execution。
type ExecutionOperations interface {
	SendInput(context.Context, executionservice.SendInput) (executionservice.SendInputResult, error)
	PrepareLoop(context.Context, identity.AgentExecutionID) (loop.Input, bool, error)
	ConfirmRunning(context.Context, identity.AgentExecutionID) error
	WaitForApproval(context.Context, identity.AgentExecutionID) error
	Settle(context.Context, identity.AgentExecutionID, agentmodel.ExecutionOutcome, agentmodel.ExecutionFailureCode, string) (agentmodel.Execution, error)
	ReclaimExecution(context.Context, agentmodel.Execution) (agentmodel.Execution, error)
	PrepareNext(context.Context, identity.AgentID) (agentmodel.Execution, bool, error)
}

// TaskQueue 是 Coordinator 在 Execution 结束后领取下一项预约任务所需的能力。
type TaskQueue interface {
	EnqueueTask(context.Context, queueservice.EnqueueInput) (queueservice.EnqueueResult, error)
}

type executionResultState struct {
	outcome agentmodel.ExecutionOutcome
	code    agentmodel.ExecutionFailureCode
	message string
}
