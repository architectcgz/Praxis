package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/loop"
	sessionservice "praxis/internal/service/session"
)

// ExecutionOperations 是 Coordinator 需要的执行准入、进度和结算能力。
type ExecutionOperations interface {
	AssistantCommitter(context.Context, identity.AgentExecutionID) (loop.AssistantCommitter, error)
	PrepareLoop(context.Context, identity.AgentExecutionID) (loop.Input, bool, error)
	ConfirmRunning(context.Context, identity.AgentExecutionID) error
	WaitForApproval(context.Context, identity.AgentExecutionID) error
	Settle(context.Context, identity.AgentExecutionID, agentmodel.ExecutionOutcome, agentmodel.ExecutionFailureCode, string) (agentmodel.Execution, error)
	ReclaimExecution(context.Context, agentmodel.Execution) (agentmodel.Execution, error)
	PrepareTask(context.Context, sessionmodel.Task, identity.AgentID, modelapi.Config) (agentmodel.Execution, bool, error)
}

// SessionTasks 提供会话输入能力；队列和正文的生命周期由 Session Service 拥有。
type SessionTasks interface {
	CreateTask(context.Context, sessionservice.TaskInput) (sessionmodel.Task, error)
	TaskQueue(identity.SessionID) (*sessionmodel.TaskQueue, bool)
	CancelTask(context.Context, identity.SessionID, identity.TaskID) error
}
