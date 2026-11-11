package run

import (
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"time"
)

// Snapshot 是初始化与事件共用的执行展示摘要，不携带配置、内部进度或运行资源。
type Snapshot struct {
	ID             identity.RunID
	Status         RunStatus
	Outcome        RunOutcome
	StartedAt      time.Time
	EndedAt        time.Time
	FailureCode    RunFailureCode
	FailureMessage string
	Iterations     uint64
	ToolCalls      int
	OutputBytes    int64
}

func (r Snapshot) Active() bool {
	return r.Status == RunStarting || r.Status == RunRunning || r.Status == RunEnding
}

func (r Snapshot) AgentState() agentmodel.AgentState {
	if r.Active() {
		return agentmodel.AgentExecuting
	}
	switch r.Outcome {
	case RunYielded:
		return agentmodel.AgentWaiting
	case RunInterrupted:
		return agentmodel.AgentInterrupted
	case RunFailed:
		return agentmodel.AgentFailed
	default:
		return agentmodel.AgentIdle
	}
}

func (r RunRecord) Snapshot() Snapshot {
	return Snapshot{
		ID:             r.ID,
		Status:         r.Status,
		Outcome:        r.Outcome,
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		FailureCode:    r.FailureCode,
		FailureMessage: r.FailureMessage,
		Iterations:     r.Progress.Iterations,
		ToolCalls:      r.Progress.ToolCalls,
		OutputBytes:    r.Progress.OutputBytes,
	}
}
