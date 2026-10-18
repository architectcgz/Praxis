package task

import (
	"praxis/internal/fault"
	"praxis/internal/identity"
	"slices"
	"time"
)

const MaxInputBytes = 32 * 1024

type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskStarting TaskStatus = "starting"
	TaskRunning  TaskStatus = "running"
	TaskEnding   TaskStatus = "ending"
	TaskEnded    TaskStatus = "ended"
)

type TaskOutcome string

const (
	TaskCompleted        TaskOutcome = "completed"
	TaskYielded          TaskOutcome = "yielded"
	TaskPaused           TaskOutcome = "paused"
	TaskFailed           TaskOutcome = "failed"
	TaskInterrupted      TaskOutcome = "interrupted"
	TaskAwaitingApproval TaskOutcome = "awaiting_approval"
)

type Task struct {
	Progress         Progress
	ID               identity.TaskID
	SessionID        identity.SessionID
	AgentID          identity.AgentID
	RequestID        identity.RequestID
	Sequence         uint64
	ProviderID       string
	ModelID          string
	ReasoningLevel   string
	Status           TaskStatus
	InputMessageID   string
	Config           Config
	ContextSelection ContextSelection
	CreatedAt        time.Time
	StartedAt        time.Time
	EndedAt          time.Time
	Outcome          TaskOutcome
	FailureCode      TaskFailureCode
	FailureMessage   string
}

// NewTask 创建空闲 runtime 可以立即启动的 Task；执行输入必须已构建完成。
func NewTask(
	id identity.TaskID,
	sessionID identity.SessionID,
	agentID identity.AgentID,
	requestID identity.RequestID,
	inputMessageID string,
	config Config,
	selection ContextSelection,
	at time.Time,
) Task {
	return Task{
		ID:               id,
		SessionID:        sessionID,
		AgentID:          agentID,
		RequestID:        requestID,
		Status:           TaskStarting,
		InputMessageID:   inputMessageID,
		Config:           config.Clone(),
		ContextSelection: selection.Clone(),
		CreatedAt:        at.UTC(),
	}
}

// NewPendingTask 创建预约 Task，只保存请求身份和 FIFO 序号；正文由用户消息保存。
func NewPendingTask(id identity.TaskID, sessionID identity.SessionID, agentID identity.AgentID, requestID identity.RequestID, inputMessageID string, sequence uint64, at time.Time) Task {
	return Task{
		ID:             id,
		SessionID:      sessionID,
		AgentID:        agentID,
		RequestID:      requestID,
		Sequence:       sequence,
		InputMessageID: inputMessageID,
		Status:         TaskPending,
		CreatedAt:      at.UTC(),
	}
}

// Prepare 在 runtime 空闲时冻结已构建的执行输入；非法状态或排除当前输入时不修改 Task。
func (e *Task) Prepare(config Config, selection ContextSelection) error {
	if e.Status == TaskStarting || !canTransition(e.Status, TaskStarting) {
		return fault.InvalidTransition("task", string(e.Status), string(TaskStarting))
	}
	if slices.Contains(selection.ExcludedMessageIDs, e.InputMessageID) {
		return fault.InvalidValue("task.contextSelection", "current input cannot be excluded")
	}
	e.Config = config.Clone()
	e.ContextSelection = selection.Clone()
	e.Status = TaskStarting
	return nil
}

func (e Task) Active() bool {
	return e.Status == TaskStarting || e.Status == TaskRunning || e.Status == TaskEnding
}

func (e *Task) MarkRunning(at time.Time) error {
	if e.Status == TaskRunning || !canTransition(e.Status, TaskRunning) {
		return fault.InvalidTransition("task", string(e.Status), string(TaskRunning))
	}
	e.Status = TaskRunning
	e.StartedAt = at.UTC()
	return nil
}

// BeginEnding 标记执行已返回；结束时间不得早于启动时间，失败时不改变状态。
func (e *Task) BeginEnding(at time.Time) error {
	if e.Status == TaskEnding || !canTransition(e.Status, TaskEnding) {
		return fault.InvalidTransition("task", string(e.Status), string(TaskEnding))
	}
	if at.Before(e.StartedAt) {
		return fault.InvalidValue("task.endingAt", "ending cannot precede start")
	}
	e.Status = TaskEnding
	return nil
}

// End 固定最终结果和结束时间；已结束的 Task 不允许重新结束或改变结果。
func (e *Task) End(
	outcome TaskOutcome,
	failureCode TaskFailureCode,
	at time.Time,
) error {
	if e.Status == TaskEnded || e.StartedAt.IsZero() || !canTransition(e.Status, TaskEnded) {
		return fault.InvalidTransition("task", string(e.Status), string(TaskEnded))
	}
	if !ValidTaskOutcome(outcome) {
		return fault.InvalidValue("task.outcome", "unknown task outcome")
	}
	if at.Before(e.StartedAt) {
		return fault.InvalidValue("task.endedAt", "ending cannot precede start")
	}
	if !ValidFailureCode(failureCode) || (outcome == TaskFailed && failureCode == "") {
		return fault.InvalidValue("task.failureCode", "unknown or missing failure code")
	}
	if failureCode == TaskFailureRequestCanceled && outcome != TaskPaused && outcome != TaskInterrupted {
		return fault.InvalidValue("task.failureCode", "request cancellation requires a paused or interrupted outcome")
	}
	e.Status = TaskEnded
	e.Outcome = outcome
	e.FailureCode = failureCode
	e.EndedAt = at.UTC()
	return nil
}

func validTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskPending, TaskStarting, TaskRunning, TaskEnding, TaskEnded:
		return true
	default:
		return false
	}
}

// ValidFailureCode 检查执行结果或持久化记录中的失败码，空值表示没有失败。
func ValidFailureCode(code TaskFailureCode) bool {
	switch code {
	case "", TaskFailureProviderUnavailable, TaskFailureProvider,
		TaskFailureTool, TaskFailurePolicyBlocked,
		TaskFailureApprovalRequired, TaskFailureResourceLimit,
		TaskFailureStorage, TaskFailureContract, TaskFailureBusy,
		TaskFailureInterrupted, TaskFailureRequestCanceled,
		TaskFailureRuntimeCancelled, TaskFailureRuntimeFailed,
		TaskFailureRuntimeInvalid:
		return true
	default:
		return false
	}
}

// ValidTaskOutcome 判断回合结果是否受支持；空值或未知值返回 false。
func ValidTaskOutcome(outcome TaskOutcome) bool {
	switch outcome {
	case TaskCompleted, TaskYielded, TaskPaused, TaskFailed, TaskInterrupted:
		return true
	default:
		return false
	}
}
