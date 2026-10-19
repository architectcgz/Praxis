package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/message"
	"praxis/internal/identity"
	"praxis/internal/loop"
	executionservice "praxis/internal/service/agent/execution"
	queueservice "praxis/internal/service/task"
)

// ExecutionOperations 是 Coordinator 需要的 Execution 用例能力，具体实现属于 agent/execution。
type ExecutionOperations interface {
	LoadProgress(context.Context, identity.AgentExecutionID) (loop.Progress, error)
	RecordIteration(context.Context, identity.AgentExecutionID, uint64) error
	RecordSummaryRequest(context.Context, identity.AgentExecutionID, uint64) error
	RecordModelOutput(context.Context, identity.AgentExecutionID, int) error
	SendInput(context.Context, executionservice.SendInput) (executionservice.SendInputResult, error)
	LoadContext(context.Context, agentmodel.Execution) (appcontext.Content, []message.Message, error)
	ConfirmRunning(context.Context, identity.AgentExecutionID) error
	WaitForApproval(context.Context, identity.AgentExecutionID) error
	Settle(context.Context, identity.AgentExecutionID, agentmodel.ExecutionOutcome, agentmodel.ExecutionFailureCode, string) (agentmodel.Execution, error)
	ReclaimExecution(context.Context, agentmodel.Execution) (agentmodel.Execution, error)
	ReconcileToolResults(context.Context) error
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

// executionProgress 将进度业务操作绑定到唯一执行，由 Execution Service 校验和提交。
type executionProgress struct {
	operations ExecutionOperations
	id         identity.AgentExecutionID
}

func (p executionProgress) LoadProgress(ctx context.Context) (loop.Progress, error) {
	return p.operations.LoadProgress(ctx, p.id)
}
func (p executionProgress) RecordIteration(ctx context.Context, sequence uint64) error {
	return p.operations.RecordIteration(ctx, p.id, sequence)
}
func (p executionProgress) RecordSummaryRequest(ctx context.Context, sequence uint64) error {
	return p.operations.RecordSummaryRequest(ctx, p.id, sequence)
}
func (p executionProgress) RecordModelOutput(ctx context.Context, count int) error {
	return p.operations.RecordModelOutput(ctx, p.id, count)
}
