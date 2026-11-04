package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/loop"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	runservice "praxis/internal/service/agent/run"
)

// RunOperations 是 Coordinator 需要的执行准入、进度和结算能力。
type RunOperations interface {
	Execute(context.Context, *agentrun.Run, *loop.Runner, loop.ToolExecutor) error
	Compact(context.Context, *agentrun.Run, *loop.Runner) error
	ConfirmRunning(context.Context, *agentrun.Run) error
	Settle(context.Context, *agentrun.Run, agentrun.RunOutcome, agentrun.RunFailureCode, string) (agentrun.RunRecord, error)
	PrepareTask(context.Context, sessionmodel.Task, identity.AgentID, agentmodel.AgentDefinition, modelapi.Config, agentrun.SessionContext, string) (*agentrun.Run, bool, error)
	PrepareSubagent(context.Context, runservice.SubagentInput) (*agentrun.Run, error)
}
