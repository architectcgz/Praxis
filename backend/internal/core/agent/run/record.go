package run

import (
	"time"

	agentmodel "praxis/internal/core/agent"
	modelusage "praxis/internal/core/model/usage"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
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
	RunCompleted   RunOutcome = "completed"
	RunYielded     RunOutcome = "yielded"
	RunFailed      RunOutcome = "failed"
	RunInterrupted RunOutcome = "interrupted"
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
	Progress    RunProgress
	CurrentTurn TurnRecord
	ID          identity.RunID
	SessionID   identity.SessionID
	// BranchID 在准入时确定主、子执行的物理归属；父执行关系由 ParentRunID 表达。
	BranchID  identity.BranchID `json:"BranchID,omitempty"`
	AgentID   identity.AgentID
	AgentName string
	// 父运行关联的日志字段名属于持久化契约，不随运行模型的命名调整。
	ParentRunID    identity.RunID `json:"ParentExecutionID"`
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
		CurrentTurn:  newTurn(id, 1, at),
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

// BeginEnding 标记执行已返回；调用方负责检查前置状态。
func (e *RunRecord) BeginEnding() {
	e.Status = RunEnding
}

// End 记录最终结果和结束时间；调用方负责前置状态、结果准入及重复结算处理。
func (e *RunRecord) End(
	outcome RunOutcome,
	failureCode RunFailureCode,
	at time.Time,
) {
	e.Status = RunEnded
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.EndedAt = at.UTC()
	e.EndTurn(at)
}

// RebuildEntryStats 仅在准入和必要恢复时从执行证据重建统计，运行中按新增条目增量推进。
func (progress RunProgress) RebuildEntryStats(runID string, entries []*sessionentry.ContextEntry) RunProgress {
	progress.OutputBytes, progress.ToolCalls = progress.ModelOutputBytes, 0
	progress.FinalAssistantID, progress.FinalStopReason = "", ""
	for _, entry := range entries {
		if entry.RunID != runID {
			continue
		}
		progress.AcceptEntry(entry)
	}
	return progress
}

// AcceptEntry 只接收首次接纳的所属条目；去重由 Run 的条目接纳入口完成。
func (progress *RunProgress) AcceptEntry(entry *sessionentry.ContextEntry) {
	if entry.Role == role.Assistant {
		progress.FinalAssistantID, progress.FinalStopReason = "", ""
		if entry.Response != nil && (entry.Response.StopReason == "completed" || entry.Response.StopReason == "stop_sequence") {
			progress.FinalAssistantID, progress.FinalStopReason = entry.ID, entry.Response.StopReason
		}
	}
	for _, block := range entry.Content {
		switch value := block.(type) {
		case sessionentry.ToolCallBlock:
			progress.ToolCalls++
		case sessionentry.ToolResultBlock:
			progress.OutputBytes += int64(len(value.Text))
		}
	}
}
