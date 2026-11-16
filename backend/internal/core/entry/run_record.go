package entry

import (
	"time"

	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/entry/role"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
)

type RunStatus string

const (
	RunStarting RunStatus = "starting"
	RunRunning  RunStatus = "running"
	RunEnding   RunStatus = "ending"
	RunEnded    RunStatus = "ended"
)

type RunOutcome string

const (
	OutcomeCompleted   RunOutcome = "completed"
	OutcomeYielded     RunOutcome = "yielded"
	OutcomeFailed      RunOutcome = "failed"
	OutcomeInterrupted RunOutcome = "interrupted"
)

// RunProgress 记录本次执行累计；结束时将消息统计与用量一并固化。
type RunProgress struct {
	Usage            modelusage.Totals
	Iterations       uint64
	ModelRequests    uint64
	ModelOutputBytes int64
	OutputBytes      int64
	ToolCalls        int
	FinalStopReason  string
	FinalAssistantID string
}

// RunRecord 保存一次执行的身份、配置、状态和进度，不持有运行时资源。
type RunRecord struct {
	Progress       RunProgress
	CurrentTurn    TurnRecord
	ID             identity.RunID
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	AgentName      string
	ParentRunID    identity.RunID
	ParentCallID   string
	SourceTaskID   identity.TaskID
	Status         RunStatus
	InputEntryID   string
	Config         RunConfig
	CreatedAt      time.Time
	StartedAt      time.Time
	EndedAt        time.Time
	Outcome        RunOutcome
	FailureCode    RunFailureCode
	FailureMessage string
}

// NewRunRecord 创建已准入的执行记录；模型上下文由 Loop 从初始历史构建。
func NewRunRecord(
	id identity.RunID,
	sessionID identity.SessionID,
	agentID identity.AgentID,
	taskID identity.TaskID,
	inputMessageID string,
	config RunConfig,
	at time.Time,
) RunRecord {
	return RunRecord{
		ID:           id,
		SessionID:    sessionID,
		AgentID:      agentID,
		SourceTaskID: taskID,
		Status:       RunStarting,
		InputEntryID: inputMessageID,
		Config:       config,
		CreatedAt:    at.UTC(),
	}
}

func (e RunRecord) Active() bool {
	return e.Status == RunStarting || e.Status == RunRunning || e.Status == RunEnding
}

// AgentState 从执行事实投影展示状态，不为历史记录创建 Agent 实例。
func (e RunRecord) AgentState() agentmodel.AgentState {
	return e.Snapshot().AgentState()
}

// Start 记录首次启动状态和时间；调用方负责确认执行尚未启动。
func (e *RunRecord) Start(at time.Time) {
	e.Status = RunRunning
	e.StartedAt = at.UTC()
}

// AcceptEntry 只接收首次接纳的所属条目；去重由 Run 的条目接纳入口完成。
func (progress *RunProgress) AcceptEntry(entry *ContextEntry) {
	if entry.Role == role.Assistant {
		progress.FinalAssistantID, progress.FinalStopReason = "", ""
		if entry.Response != nil && (entry.Response.StopReason == "completed" || entry.Response.StopReason == "stop_sequence") {
			progress.FinalAssistantID, progress.FinalStopReason = entry.ID, entry.Response.StopReason
		}
	}
	for _, block := range entry.Content {
		switch value := block.(type) {
		case ToolCallBlock:
			progress.ToolCalls++
			progress.FinalAssistantID, progress.FinalStopReason = "", ""
		case ToolResultBlock:
			progress.OutputBytes += int64(len(value.Text))
		}
	}
}
