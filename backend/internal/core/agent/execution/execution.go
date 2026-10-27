package execution

import (
	"time"

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
