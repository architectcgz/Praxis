package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/core/loop"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	executionservice "praxis/internal/service/agent/execution"
)

// ExecutionOperations 是 Coordinator 需要的执行准入、进度和结算能力。
type ExecutionOperations interface {
	ListAgentTimings(context.Context, identity.SessionID, identity.AgentID, identity.BranchID, int) ([]timing.View, error)
	Execute(context.Context, *agentexecution.Run, *loop.Runner, loop.ToolExecutor) error
	Compact(context.Context, *agentexecution.Run, *loop.Runner) error
	ConfirmRunning(context.Context, *agentexecution.Run) error
	Settle(context.Context, *agentexecution.Run, agentexecution.ExecutionOutcome, agentexecution.ExecutionFailureCode, string) (agentexecution.ExecutionRecord, error)
	PrepareTask(context.Context, sessionmodel.Task, identity.AgentID, agentmodel.AgentDefinition, modelapi.Config, agentexecution.SessionContext, string) (*agentexecution.Run, bool, error)
	PrepareSubagent(context.Context, executionservice.SubagentInput) (*agentexecution.Run, error)
}
