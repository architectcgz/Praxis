package agent

import (
	"praxis/internal/fault"
	"praxis/internal/identity"
	"time"
)

const MaxInputBytes = 32 * 1024

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

type Execution struct {
	Progress         ExecutionProgress
	ID               identity.AgentExecutionID
	SessionID        identity.SessionID
	AgentID          identity.AgentID
	RequestID        identity.RequestID
	SourceTaskID     identity.TaskID
	ProviderID       string
	ModelID          string
	ReasoningLevel   string
	Status           ExecutionStatus
	InputMessageID   string
	Config           ExecutionConfig
	ContextSelection ContextSelection
	CreatedAt        time.Time
	StartedAt        time.Time
	EndedAt          time.Time
	Outcome          ExecutionOutcome
	FailureCode      ExecutionFailureCode
	FailureMessage   string
}

// NewExecution 创建空闲 runtime 可以立即启动的 Execution；执行输入必须已构建完成。
func NewExecution(
	id identity.AgentExecutionID,
	sessionID identity.SessionID,
	agentID identity.AgentID,
	requestID identity.RequestID,
	inputMessageID string,
	config ExecutionConfig,
	selection ContextSelection,
	at time.Time,
) Execution {
	return Execution{
		ID:               id,
		SessionID:        sessionID,
		AgentID:          agentID,
		RequestID:        requestID,
		Status:           ExecutionStarting,
		InputMessageID:   inputMessageID,
		Config:           config.Clone(),
		ContextSelection: selection.Clone(),
		CreatedAt:        at.UTC(),
	}
}

func (e Execution) Active() bool {
	return e.Status == ExecutionStarting || e.Status == ExecutionRunning || e.Status == ExecutionWaitingApproval || e.Status == ExecutionEnding
}

func (e *Execution) MarkRunning(at time.Time) error {
	if e.Status == ExecutionRunning || !canExecutionTransition(e.Status, ExecutionRunning) {
		return fault.InvalidTransition("task", string(e.Status), string(ExecutionRunning))
	}
	e.Status = ExecutionRunning
	if e.StartedAt.IsZero() {
		e.StartedAt = at.UTC()
	}
	return nil
}

// BeginEnding 标记执行已返回；结束时间不得早于启动时间，失败时不改变状态。
func (e *Execution) BeginEnding(at time.Time) error {
	if e.Status == ExecutionEnding || !canExecutionTransition(e.Status, ExecutionEnding) {
		return fault.InvalidTransition("task", string(e.Status), string(ExecutionEnding))
	}
	if at.Before(e.StartedAt) {
		return fault.InvalidValue("task.endingAt", "ending cannot precede start")
	}
	e.Status = ExecutionEnding
	return nil
}

// End 固定最终结果和结束时间；已结束的 Execution 不允许重新结束或改变结果。
func (e *Execution) End(
	outcome ExecutionOutcome,
	failureCode ExecutionFailureCode,
	at time.Time,
) error {
	if e.Status == ExecutionEnded || e.StartedAt.IsZero() || !canExecutionTransition(e.Status, ExecutionEnded) {
		return fault.InvalidTransition("task", string(e.Status), string(ExecutionEnded))
	}
	if !ValidExecutionOutcome(outcome) {
		return fault.InvalidValue("task.outcome", "unknown task outcome")
	}
	if at.Before(e.StartedAt) {
		return fault.InvalidValue("task.endedAt", "ending cannot precede start")
	}
	if !ValidFailureCode(failureCode) || (outcome == ExecutionFailed && failureCode == "") {
		return fault.InvalidValue("task.failureCode", "unknown or missing failure code")
	}
	if failureCode == ExecutionFailureRequestCanceled && outcome != ExecutionPaused && outcome != ExecutionInterrupted {
		return fault.InvalidValue("task.failureCode", "request cancellation requires a paused or interrupted outcome")
	}
	e.Status = ExecutionEnded
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.EndedAt = at.UTC()
	return nil
}

func validExecutionStatus(status ExecutionStatus) bool {
	switch status {
	case ExecutionStarting, ExecutionRunning, ExecutionWaitingApproval, ExecutionEnding, ExecutionEnded:
		return true
	default:
		return false
	}
}

// ValidFailureCode 检查执行结果或持久化记录中的失败码，空值表示没有失败。
func ValidFailureCode(code ExecutionFailureCode) bool {
	switch code {
	case "", ExecutionFailureProviderUnavailable, ExecutionFailureProvider,
		ExecutionFailureTool, ExecutionFailurePolicyBlocked,
		ExecutionFailureApprovalRequired, ExecutionFailureResourceLimit,
		ExecutionFailureStorage, ExecutionFailureContract, ExecutionFailureBusy,
		ExecutionFailureInterrupted, ExecutionFailureRequestCanceled,
		ExecutionFailureCancelled, ExecutionFailureInternal,
		ExecutionFailureInvalidOutcome:
		return true
	default:
		return false
	}
}

// ValidExecutionOutcome 判断回合结果是否受支持；空值或未知值返回 false。
func ValidExecutionOutcome(outcome ExecutionOutcome) bool {
	switch outcome {
	case ExecutionCompleted, ExecutionYielded, ExecutionPaused, ExecutionFailed, ExecutionInterrupted:
		return true
	default:
		return false
	}
}

// WaitForApproval 保留执行归属和已冻结输入，不产生终态。
func (e *Execution) WaitForApproval() error {
	if !canExecutionTransition(e.Status, ExecutionWaitingApproval) {
		return fault.ErrRequestConflict
	}
	e.Status = ExecutionWaitingApproval
	return nil
}
