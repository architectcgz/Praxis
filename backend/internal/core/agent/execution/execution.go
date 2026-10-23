package execution

import (
	"time"

	"praxis/internal/identity"
)

type ExecutionStatus string

const (
	ExecutionStarting        ExecutionStatus = "starting"
	ExecutionRunning         ExecutionStatus = "running"
	ExecutionWaitingApproval ExecutionStatus = "waiting_approval"
	ExecutionEnding          ExecutionStatus = "ending"
	ExecutionEnded           ExecutionStatus = "ended"
)

type ExecutionOutcome string

const (
	ExecutionCompleted   ExecutionOutcome = "completed"
	ExecutionYielded     ExecutionOutcome = "yielded"
	ExecutionPaused      ExecutionOutcome = "paused"
	ExecutionFailed      ExecutionOutcome = "failed"
	ExecutionInterrupted ExecutionOutcome = "interrupted"
)

// ExecutionProgress 保存模型请求消耗；消息相关统计由仓储派生，重启不能重置预算。
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

type Execution struct {
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

// NewExecution 创建已准入的 Execution；模型上下文在执行准备阶段构建。
func NewExecution(
	id identity.AgentExecutionID,
	sessionID identity.SessionID,
	agentID identity.AgentID,
	requestID identity.RequestID,
	inputMessageID string,
	config ExecutionConfig,
	at time.Time,
) Execution {
	return Execution{
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

func (e Execution) Active() bool {
	return e.Status == ExecutionStarting || e.Status == ExecutionRunning || e.Status == ExecutionWaitingApproval || e.Status == ExecutionEnding
}

// Start 记录首次启动状态和时间；调用方负责确认执行尚未启动。
func (e *Execution) Start(at time.Time) {
	e.Status = ExecutionRunning
	e.StartedAt = at.UTC()
}

// Resume 恢复运行状态并保留首次启动时间；调用方负责确认执行允许恢复。
func (e *Execution) Resume() {
	e.Status = ExecutionRunning
}

// BeginEnding 标记执行已返回；调用方负责检查前置状态。
func (e *Execution) BeginEnding() {
	e.Status = ExecutionEnding
}

// End 记录最终结果和结束时间；调用方负责前置状态、结果准入及重复结算处理。
func (e *Execution) End(
	outcome ExecutionOutcome,
	failureCode ExecutionFailureCode,
	at time.Time,
) {
	e.Status = ExecutionEnded
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.EndedAt = at.UTC()
}

// WaitForApproval 记录审批等待状态；调用方负责确认执行允许等待审批。
func (e *Execution) WaitForApproval() {
	e.Status = ExecutionWaitingApproval
}
