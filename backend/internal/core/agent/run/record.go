package run

import (
	"fmt"
	"strings"
	"time"

	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
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

// RunProgress 在内存中记录模型请求消耗；消息统计由已接纳 Entry 派生，重启只能恢复已落盘的预算。
type RunProgress struct {
	Usage            UsageTotals
	NextSequence     uint64
	Iterations       uint64
	ModelRequests    uint64
	ModelOutputBytes int64
	OutputBytes      int64  `json:"-"`
	ToolCalls        int    `json:"-"`
	FinalStopReason  string `json:"-"`
	FinalAssistantID string `json:"-"`
}

// RunRecord 保存一次执行的身份、配置、状态和进度，不持有运行时资源。
type RunRecord struct {
	Progress  RunProgress
	ID        identity.RunID
	SessionID identity.SessionID
	BranchID  identity.BranchID
	AgentID   identity.AgentID
	AgentName string
	// 父运行关联的日志字段名属于持久化契约，不随运行模型的命名调整。
	ParentRunID    identity.RunID `json:"ParentExecutionID"`
	ParentCallID   string
	RequestID      identity.RequestID
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
	requestID identity.RequestID,
	inputMessageID string,
	config RunConfig,
	at time.Time,
) RunRecord {
	return RunRecord{
		ID:           id,
		SessionID:    sessionID,
		AgentID:      agentID,
		RequestID:    requestID,
		Status:       RunStarting,
		InputEntryID: inputMessageID,
		Config:       config.Clone(),
		CreatedAt:    at.UTC(),
	}
}

func (e RunRecord) Active() bool {
	return e.Status == RunStarting || e.Status == RunRunning || e.Status == RunEnding
}

// AgentState 从执行事实投影展示状态，不为历史记录创建 Agent 实例。
func (e RunRecord) AgentState() agentmodel.AgentState {
	if e.Active() {
		return agentmodel.AgentExecuting
	}
	switch e.Outcome {
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
}

// Validate 在日志读取边界检查必需引用、枚举值和配置格式。
func (e RunRecord) Validate() error {
	for _, id := range []string{
		e.ID.String(),
		e.SessionID.String(),
		e.BranchID.String(),
		e.AgentID.String(),
		e.RequestID.String(),
		e.InputEntryID,
	} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("agent_execution: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	switch e.Status {
	case RunStarting, RunRunning, RunEnding, RunEnded:
	default:
		return fmt.Errorf("agent_execution: unknown status: %w", errorcode.ErrInvalidValue)
	}
	if err := e.Config.Validate(); err != nil {
		return fmt.Errorf("execution.config: %w", err)
	}

	if e.Status == RunEnded {
		switch e.Outcome {
		case RunCompleted, RunYielded, RunFailed, RunInterrupted:
		default:
			return fmt.Errorf("agent_execution: unknown outcome: %w", errorcode.ErrInvalidValue)
		}
	}
	return nil
}

// WithEntries 从所属执行已接纳的 Entry 派生工具计数、输出字节和最终回答证据。
func (progress RunProgress) WithEntries(runID string, entries []*sessionentry.ContextEntry) RunProgress {
	progress.OutputBytes, progress.ToolCalls = progress.ModelOutputBytes, 0
	progress.FinalAssistantID, progress.FinalStopReason = "", ""
	var last sessionentry.ContextEntry
	for _, entry := range entries {
		if entry.RunID != runID {
			continue
		}
		if entry.Role == role.Assistant {
			last = *entry
		}
		for _, block := range entry.Content {
			if block.Kind == sessionentry.BlockToolCall {
				progress.ToolCalls++
			}
			if block.Kind == sessionentry.BlockToolResult {
				progress.OutputBytes += int64(len(block.Text))
			}
		}
	}
	if last.StopReason == "completed" || last.StopReason == "stop_sequence" {
		progress.FinalAssistantID, progress.FinalStopReason = last.ID, last.StopReason
	}
	return progress
}
