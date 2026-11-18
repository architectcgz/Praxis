package entry

import (
	agentmodel "praxis/internal/core/agent"
	entryPayload "praxis/internal/core/entry/payload"
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
	Outcome        entryPayload.RunOutcome
	EndedAt        time.Time
	FailureCode    entryPayload.RunFailureCode
	FailureMessage string
}

// AgentStateFromOutcome 将 Run 终态映射到 Agent 的展示状态。
func AgentStateFromOutcome(outcome entryPayload.RunOutcome) agentmodel.AgentState {
	switch outcome {
	case entryPayload.OutcomeInterrupted:
		return agentmodel.AgentInterrupted
	case entryPayload.OutcomeFailed:
		return agentmodel.AgentFailed
	default:
		return agentmodel.AgentIdle
	}
}

// SnapshotFromTerminal 为查询和通知投影已提交的 Run 终态。
func SnapshotFromTerminal(id identity.RunID, terminal entryPayload.RunTerminal) RunSnapshot {
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
	_, ok := e.Payload.(entryPayload.RunTerminal)
	return ok
}
