package coordinator

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/core/loop"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// ExecutionOperations 是 Coordinator 需要的执行准入、进度和结算能力。
type ExecutionOperations interface {
	ListAgentTimings(context.Context, string, int) ([]timing.View, error)
	RestoreRun(context.Context, agentexecution.Execution) (*agentexecution.Run, error)
	PrepareLoop(context.Context, *agentexecution.Run) (loop.Input, bool, error)
	ConfirmRunning(context.Context, *agentexecution.Run) error
	Settle(context.Context, *agentexecution.Run, agentexecution.ExecutionOutcome, agentexecution.ExecutionFailureCode, string) (agentexecution.Execution, error)
	PrepareTask(context.Context, sessionmodel.Task, identity.AgentID, modelapi.Config) (*agentexecution.Run, bool, error)
}
