package coordinator

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/core/loop"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

// ExecutionOperations 是 Coordinator 需要的执行准入、进度和结算能力。
type ExecutionOperations interface {
	ListAgentTimings(context.Context, string, int) ([]timing.View, error)
	RestoreRuntime(context.Context, agentexecution.Execution) (*agentexecution.Runtime, error)
	PrepareLoop(context.Context, *agentexecution.Runtime) (loop.Input, bool, error)
	ConfirmRunning(context.Context, *agentexecution.Runtime) error
	Settle(context.Context, *agentexecution.Runtime, agentexecution.ExecutionOutcome, agentexecution.ExecutionFailureCode, string) (agentexecution.Execution, error)
	PrepareTask(context.Context, sessionmodel.Task, identity.AgentID, modelapi.Config) (*agentexecution.Runtime, bool, error)
}

// SessionTasks 提供会话输入能力；队列和正文的生命周期由 Session Service 拥有。
type SessionTasks interface {
	CreateTask(context.Context, sessionservice.TaskInput) (sessionmodel.Task, error)
	TaskQueue(identity.SessionID) (*sessionmodel.TaskQueue, bool)
	CancelTask(context.Context, identity.SessionID, identity.TaskID) error
}
