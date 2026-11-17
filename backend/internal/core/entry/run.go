package entry

import (
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/entry/role"
	modelapi "praxis/internal/core/model"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
	"time"
)

// RunConfig 持有所选的只读会话配置，模型能力列表共享底层数据。
type RunConfig struct {
	AgentDefinitionID identity.AgentDefinitionID
	SystemPrompt      string
	Model             modelapi.Config
}

// TurnRecord 表达一轮 ReAct 迭代，请求尝试及审核不单独创建轮次。
type TurnRecord struct {
	ID        identity.TurnID
	RunID     identity.RunID
	StartedAt time.Time
	EndedAt   time.Time
}

// RunFailureCode 是任务持久化结果的稳定失败原因。
type RunFailureCode string

const (
	RunFailureTimeout             RunFailureCode = "execution_timeout"
	RunFailureProviderUnavailable RunFailureCode = "provider_unavailable"
	RunFailureProvider            RunFailureCode = "execution_provider_error"
	RunFailureTool                RunFailureCode = "execution_tool_error"
	RunFailurePolicyBlocked       RunFailureCode = "execution_policy_blocked"
	RunFailureApprovalRequired    RunFailureCode = "execution_approval_required"
	RunFailureResourceLimit       RunFailureCode = "execution_resource_limit"
	RunFailureStorage             RunFailureCode = "execution_storage_error"
	RunFailureContract            RunFailureCode = "execution_contract_error"
	RunFailureBusy                RunFailureCode = "execution_busy"
	RunFailureInterrupted         RunFailureCode = "execution_interrupted"
	RunFailureRequestCanceled     RunFailureCode = "request_canceled"
	RunFailureCancelled           RunFailureCode = "execution_cancelled"
	RunFailureInternal            RunFailureCode = "execution_failed"
	RunFailureInvalidOutcome      RunFailureCode = "execution_invalid_outcome"
)

// MaxRunFailureMessageRunes 限制持久化失败详情的字符数。
const MaxRunFailureMessageRunes = 4096

// RunResult 是完成收尾后发布的不可变执行结果，不包含冻结配置或模型上下文。
type RunResult struct {
	Outcome          string            `json:"outcome"`
	FailureCode      string            `json:"failureCode,omitempty"`
	FailureMessage   string            `json:"failureMessage,omitempty"`
	StartedAt        time.Time         `json:"startedAt"`
	EndedAt          time.Time         `json:"endedAt"`
	Usage            modelusage.Totals `json:"usage"`
	ModelRequests    uint64            `json:"modelRequests"`
	Iterations       uint64            `json:"iterations"`
	OutputBytes      int64             `json:"outputBytes"`
	ToolCalls        int               `json:"toolCalls"`
	FinalAssistantID string            `json:"finalAssistantId,omitempty"`
	FinalStopReason  string            `json:"finalStopReason,omitempty"`
}

func (e ContextEntry) IsRun() bool { return e.ExecutionResult() != nil }

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

// HistorySummary 是可重建的执行目录，不包含冻结配置或历史正文。
// 完整 RunRecord 仅在执行和必要恢复中读取，展示与累计使用该投影。
type HistorySummary struct {
	RunSnapshot
	SessionID     identity.SessionID
	AgentID       identity.AgentID
	AgentName     string
	DefinitionID  identity.AgentDefinitionID
	ParentRunID   identity.RunID
	SourceTaskID  identity.TaskID
	InputEntryID  string
	CreatedAt     time.Time
	Usage         modelusage.Totals
	ModelRequests uint64
}

func (r RunRecord) HistorySummary() HistorySummary {
	return HistorySummary{
		RunSnapshot:   r.Snapshot(),
		SessionID:     r.SessionID,
		AgentID:       r.AgentID,
		AgentName:     r.AgentName,
		DefinitionID:  r.Config.AgentDefinitionID,
		ParentRunID:   r.ParentRunID,
		SourceTaskID:  r.SourceTaskID,
		InputEntryID:  r.InputEntryID,
		CreatedAt:     r.CreatedAt,
		Usage:         r.Progress.Usage,
		ModelRequests: r.Progress.ModelRequests,
	}
}
