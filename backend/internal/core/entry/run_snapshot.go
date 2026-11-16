package entry

import (
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"time"
)

// RunSnapshot 是初始化与事件共用的执行展示摘要，不携带配置、内部进度或运行资源。
type RunSnapshot struct {
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

func (r RunSnapshot) Active() bool {
	return r.Status == RunStarting || r.Status == RunRunning || r.Status == RunEnding
}

func (r RunSnapshot) AgentState() agentmodel.AgentState {
	if r.Active() {
		return agentmodel.AgentExecuting
	}
	switch r.Outcome {
	case OutcomeInterrupted:
		return agentmodel.AgentInterrupted
	case OutcomeFailed:
		return agentmodel.AgentFailed
	default:
		return agentmodel.AgentIdle
	}
}

func (r RunRecord) Snapshot() RunSnapshot {
	return RunSnapshot{
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
