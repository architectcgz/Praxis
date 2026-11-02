package execution

import (
	"fmt"
	"strings"
	"time"

	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/agent/execution/timing"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type ExecutionStatus string

const (
	ExecutionStarting ExecutionStatus = "starting"
	ExecutionRunning  ExecutionStatus = "running"
	ExecutionEnding   ExecutionStatus = "ending"
	ExecutionEnded    ExecutionStatus = "ended"
)

type ExecutionOutcome string

const (
	ExecutionCompleted   ExecutionOutcome = "completed"
	ExecutionYielded     ExecutionOutcome = "yielded"
	ExecutionFailed      ExecutionOutcome = "failed"
	ExecutionInterrupted ExecutionOutcome = "interrupted"
)

// ExecutionProgress 在内存中记录模型请求消耗；消息统计由已接纳 Entry 派生，重启只能恢复已落盘的预算。
type ExecutionProgress struct {
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

// ExecutionRecord 保存一次执行的身份、配置、状态和进度，不持有运行时资源。
type ExecutionRecord struct {
	Progress          ExecutionProgress
	ID                identity.AgentExecutionID
	SessionID         identity.SessionID
	BranchID          identity.BranchID
	AgentID           identity.AgentID
	AgentName         string
	ParentExecutionID identity.AgentExecutionID
	ParentCallID      string
	RequestID         identity.RequestID
	SourceTaskID      identity.TaskID
	Status            ExecutionStatus
	InputEntryID      string
	Config            ExecutionConfig
	CreatedAt         time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Outcome           ExecutionOutcome
	FailureCode       ExecutionFailureCode
	FailureMessage    string
}

// NewExecutionRecord 创建已准入的执行记录；模型上下文在执行准备阶段构建。
func NewExecutionRecord(
	id identity.AgentExecutionID,
	sessionID identity.SessionID,
	agentID identity.AgentID,
	requestID identity.RequestID,
	inputMessageID string,
	config ExecutionConfig,
	at time.Time,
) ExecutionRecord {
	return ExecutionRecord{
		ID:           id,
		SessionID:    sessionID,
		AgentID:      agentID,
		RequestID:    requestID,
		Status:       ExecutionStarting,
		InputEntryID: inputMessageID,
		Config:       config.Clone(),
		CreatedAt:    at.UTC(),
	}
}

func (e ExecutionRecord) Active() bool {
	return e.Status == ExecutionStarting || e.Status == ExecutionRunning || e.Status == ExecutionEnding
}

// AgentState 从执行事实投影展示状态，不为历史记录创建 Agent 实例。
func (e ExecutionRecord) AgentState() agentmodel.AgentState {
	if e.Active() {
		return agentmodel.AgentExecuting
	}
	switch e.Outcome {
	case ExecutionYielded:
		return agentmodel.AgentWaiting
	case ExecutionInterrupted:
		return agentmodel.AgentInterrupted
	case ExecutionFailed:
		return agentmodel.AgentFailed
	default:
		return agentmodel.AgentIdle
	}
}

// Start 记录首次启动状态和时间；调用方负责确认执行尚未启动。
func (e *ExecutionRecord) Start(at time.Time) {
	e.Status = ExecutionRunning
	e.StartedAt = at.UTC()
}

// BeginEnding 标记执行已返回；调用方负责检查前置状态。
func (e *ExecutionRecord) BeginEnding() {
	e.Status = ExecutionEnding
}

// End 记录最终结果和结束时间；调用方负责前置状态、结果准入及重复结算处理。
func (e *ExecutionRecord) End(
	outcome ExecutionOutcome,
	failureCode ExecutionFailureCode,
	at time.Time,
) {
	e.Status = ExecutionEnded
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.EndedAt = at.UTC()
}

// Validate 在日志读取边界检查必需引用、枚举值和配置格式。
func (e ExecutionRecord) Validate() error {
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
	case ExecutionStarting, ExecutionRunning, ExecutionEnding, ExecutionEnded:
	default:
		return fmt.Errorf("agent_execution: unknown status: %w", errorcode.ErrInvalidValue)
	}
	if err := e.Config.Validate(); err != nil {
		return fmt.Errorf("execution.config: %w", err)
	}

	if e.Status == ExecutionEnded {
		switch e.Outcome {
		case ExecutionCompleted, ExecutionYielded, ExecutionFailed, ExecutionInterrupted:
		default:
			return fmt.Errorf("agent_execution: unknown outcome: %w", errorcode.ErrInvalidValue)
		}
	}
	return nil
}

// TimingView 从 Execution 生命周期投影执行总耗时，操作 Span 由独立计时记录投影。
func (e ExecutionRecord) TimingView() *timing.View {
	if e.StartedAt.IsZero() {
		return nil
	}
	value := &timing.View{
		ID:          "execution:" + e.ID.String(),
		SessionID:   e.SessionID.String(),
		AgentID:     e.AgentID.String(),
		ExecutionID: e.ID.String(),
		Kind:        "agent",
		Name:        e.Config.AgentDefinitionID.String(),
		StartedAt:   e.StartedAt,
		Status:      string(timing.Running),
		Revision:    1,
	}
	if e.EndedAt.IsZero() {
		return value
	}
	elapsed := e.EndedAt.Sub(e.StartedAt).Milliseconds()
	value.DurationMS = new(elapsed)
	value.FinishedAt = e.EndedAt
	value.Revision = 2
	value.Status = string(timing.Completed)
	switch e.Outcome {
	case ExecutionFailed:
		value.Status = string(timing.Failed)
	case ExecutionInterrupted:
		value.Status = string(timing.Cancelled)
	}
	return value
}

// WithEntries 从所属执行已接纳的 Entry 派生工具计数、输出字节和最终回答证据。
func (progress ExecutionProgress) WithEntries(executionID string, entries []*sessionentry.ContextEntry) ExecutionProgress {
	progress.OutputBytes, progress.ToolCalls = progress.ModelOutputBytes, 0
	progress.FinalAssistantID, progress.FinalStopReason = "", ""
	var last sessionentry.ContextEntry
	for _, entry := range entries {
		if entry.ExecutionID != executionID {
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
