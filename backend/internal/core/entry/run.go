package entry

import (
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"time"
)

type RunStatus string

const (
	RunStarting RunStatus = "starting"
	RunRunning  RunStatus = "running"
	RunEnding   RunStatus = "ending"
	RunEnded    RunStatus = "ended"
)

// RunSnapshot 是 Run 当前阶段、执行累计及已提交结果的只读快照。
type RunSnapshot struct {
	ID          identity.RunID
	Status      RunStatus
	StartedAt   time.Time
	Iterations  uint64
	ToolCalls   int
	OutputBytes int64
	Result      *RunConclusion
}

type RunConclusion struct {
	Outcome        RunOutcome
	EndedAt        time.Time
	FailureCode    RunFailureCode
	FailureMessage string
}

// AgentStateFromOutcome 将 Run 终态映射到 Agent 的展示状态。
func AgentStateFromOutcome(outcome RunOutcome) agentmodel.AgentState {
	switch outcome {
	case OutcomeInterrupted:
		return agentmodel.AgentInterrupted
	case OutcomeFailed:
		return agentmodel.AgentFailed
	default:
		return agentmodel.AgentIdle
	}
}

// SnapshotFromTerminal 为查询和通知投影已提交的 Run 终态。
func SnapshotFromTerminal(id identity.RunID, terminal RunTerminal) RunSnapshot {
	stats := terminal.Stats()
	code, message := terminal.Failure()
	return RunSnapshot{
		ID:          id,
		Status:      RunEnded,
		StartedAt:   stats.StartedAt,
		Iterations:  stats.Iterations,
		ToolCalls:   stats.ToolCalls,
		OutputBytes: stats.OutputBytes,
		Result: &RunConclusion{
			Outcome:        terminal.Outcome(),
			EndedAt:        stats.EndedAt,
			FailureCode:    code,
			FailureMessage: message,
		},
	}
}

func (e Entry) IsRun() bool {
	_, ok := e.Payload.(RunTerminal)
	return ok
}
