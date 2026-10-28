package execution

import (
	"fmt"
	"strings"
	"time"

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
	ExecutionPaused      ExecutionOutcome = "paused"
	ExecutionFailed      ExecutionOutcome = "failed"
	ExecutionInterrupted ExecutionOutcome = "interrupted"
)

// ExecutionProgress 在内存中记录模型请求消耗；消息统计由历史派生，重启只能恢复已落盘的预算。
type ExecutionProgress struct {
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
	Progress       ExecutionProgress
	ID             identity.AgentExecutionID
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	RequestID      identity.RequestID
	SourceTaskID   identity.TaskID
	Status         ExecutionStatus
	InputEntryID   string
	Config         ExecutionConfig
	CreatedAt      time.Time
	StartedAt      time.Time
	EndedAt        time.Time
	Outcome        ExecutionOutcome
	FailureCode    ExecutionFailureCode
	FailureMessage string
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

// Validate 检查对象自身的完整性，不修改或修复输入。
func (e ExecutionRecord) Validate() error {
	if e.Progress.ModelOutputBytes < 0 || e.Progress.Iterations > e.Progress.ModelRequests || e.Progress.ModelRequests > 0 && e.Progress.NextSequence != e.Progress.ModelRequests+1 {
		return fmt.Errorf("execution.progress: usage counters or request sequence are invalid: %w", errorcode.ErrInvalidValue)
	}
	for _, id := range []string{
		e.ID.String(),
		e.SessionID.String(),
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

	if e.CreatedAt.IsZero() || !e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt) || !e.EndedAt.IsZero() && e.EndedAt.Before(e.CreatedAt) {
		return fmt.Errorf("execution.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	if e.Status == ExecutionStarting && (!e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fmt.Errorf("agent_execution: starting execution has terminal fields: %w", errorcode.ErrInvalidValue)
	}
	if (e.Status == ExecutionRunning || e.Status == ExecutionEnding) && (e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "") {
		return fmt.Errorf("agent_execution: active execution has invalid state timestamps: %w", errorcode.ErrInvalidValue)
	}
	if e.Status == ExecutionEnded {
		if e.StartedAt.IsZero() || e.EndedAt.IsZero() || e.Outcome == ExecutionFailed && e.FailureCode == "" {
			return fmt.Errorf("agent_execution: ended execution has invalid terminal fields: %w", errorcode.ErrInvalidValue)
		}
		switch e.Outcome {
		case ExecutionCompleted, ExecutionYielded, ExecutionPaused,
			ExecutionFailed, ExecutionInterrupted:
		default:
			return fmt.Errorf("agent_execution: ended execution has invalid terminal fields: %w", errorcode.ErrInvalidValue)
		}
	}
	if e.FailureCode == ExecutionFailureRequestCanceled && e.Outcome != ExecutionPaused && e.Outcome != ExecutionInterrupted {
		return fmt.Errorf("execution.failureCode: failure code does not match outcome: %w", errorcode.ErrInvalidValue)
	}
	if len([]rune(e.FailureMessage)) > MaxExecutionFailureMessageRunes || e.FailureMessage != "" && (e.Status != ExecutionEnded || e.Outcome != ExecutionFailed) {
		return fmt.Errorf("execution.failureMessage: failure message is invalid: %w", errorcode.ErrInvalidValue)
	}
	return nil
}

// WithEntries 从所属执行的历史条目派生工具计数、输出字节和最终回答证据。
func (progress ExecutionProgress) WithEntries(executionID string, entries []sessionentry.ContextEntry) ExecutionProgress {
	progress.OutputBytes, progress.ToolCalls = progress.ModelOutputBytes, 0
	progress.FinalAssistantID, progress.FinalStopReason = "", ""
	var last sessionentry.ContextEntry
	for _, entry := range entries {
		if entry.ExecutionID != executionID {
			continue
		}
		if entry.Role == role.Assistant && entry.Sequence > last.Sequence {
			last = entry
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
