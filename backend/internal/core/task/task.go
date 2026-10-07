package task

import (
	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"

	"reflect"
	"strings"
	"time"

	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
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
	TaskCompleted   TaskOutcome = "completed"
	TaskYielded     TaskOutcome = "yielded"
	TaskPaused      TaskOutcome = "paused"
	TaskFailed      TaskOutcome = "failed"
	TaskInterrupted TaskOutcome = "interrupted"
)

// InputSnapshot 冻结一次 task 激活所需的全部持久化输入。
type InputSnapshot struct {
	AgentDefinitionID       contracts.AgentDefinitionID
	AgentDefinitionRevision string
	ContextDigest           string
	MessageSequenceBoundary uint64 `json:"messageSequenceBoundary"`
	CurrentInputMessageID   string `json:"currentInputMessageId"`
	Context                 appcontext.ModelContext
	Model                   model.ModelSnapshot
	WorkspacePath           string
	Security                contracts.SecuritySnapshot
}

func (s InputSnapshot) Validate() error {
	if !s.AgentDefinitionID.Valid() || s.AgentDefinitionRevision == "" {
		return contracts.InvalidValue("taskInput.agentDefinition", "definition id and revision are required")
	}
	if s.ContextDigest == "" {
		return contracts.InvalidValue("taskInput.context", "context digest is required")
	}
	if err := s.Context.Validate(); err != nil {
		return contracts.FieldError("taskInput.context", err)
	}
	digest, err := s.Context.Digest()
	if err != nil {
		return contracts.FieldError("taskInput.context", err)
	}
	if digest != s.ContextDigest {
		return contracts.InvalidValue("taskInput.contextDigest", "context digest does not match context")
	}
	if err := s.Model.Validate(); err != nil {
		return contracts.FieldError("taskInput.model", err)
	}
	if !pathutil.IsAbsoluteNormalized(s.WorkspacePath) {
		return contracts.InvalidValue("taskInput.workspacePath", "workspace path must be an absolute normalized path")
	}
	if err := s.Security.Validate(); err != nil {
		return contracts.FieldError("taskInput.security", err)
	}
	return nil
}

type Task struct {
	ID             contracts.TaskID
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	RequestID      contracts.RequestID
	Sequence       uint64
	ProviderID     string
	ModelID        string
	ReasoningLevel string
	Status         TaskStatus
	Input          InputSnapshot
	CreatedAt      time.Time
	StartedAt      time.Time
	EndedAt        time.Time
	Outcome        TaskOutcome
	FailureCode    contracts.TaskFailureCode
	FailureMessage string
}

// NewTask 创建空闲 runtime 可以立即启动的 Task；输入快照必须已构建完成。
func NewTask(
	id contracts.TaskID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	requestID contracts.RequestID,
	input InputSnapshot,
	at time.Time,
) (Task, error) {
	task := Task{
		ID:        id,
		SessionID: sessionID,
		AgentID:   agentID,
		RequestID: requestID,
		Status:    TaskStarting,
		Input:     input,
		CreatedAt: at.UTC(),
	}
	if err := task.Validate(); err != nil {
		return Task{}, err
	}
	return task, nil
}

// NewPendingTask 创建预约 Task，只保存请求身份和 FIFO 序号；正文由用户消息保存。
func NewPendingTask(id contracts.TaskID, sessionID contracts.SessionID, agentID contracts.AgentID, requestID contracts.RequestID, sequence uint64, at time.Time) (Task, error) {
	task := Task{
		ID:        id,
		SessionID: sessionID,
		AgentID:   agentID,
		RequestID: requestID,
		Sequence:  sequence,
		Status:    TaskPending,
		CreatedAt: at.UTC(),
	}
	if err := task.Validate(); err != nil {
		return Task{}, err
	}
	return task, nil
}

// Prepare 在 runtime 空闲时冻结输入快照；校验失败不改变待执行 Task。
func (e *Task) Prepare(input InputSnapshot) error {
	if e.Status != TaskPending {
		return contracts.InvalidTransition("task", string(e.Status), string(TaskStarting))
	}
	if err := input.Validate(); err != nil {
		return err
	}
	e.Input = input
	e.Status = TaskStarting
	return nil
}

// Validate 校验 Task 引用、输入快照和生命周期；字段规范性由输入及恢复边界保证。
func (e Task) Validate() error {
	if e.ID == "" || e.SessionID == "" || e.AgentID == "" || e.RequestID == "" {
		return contracts.InvalidValue("task", "required reference is missing")
	}
	if !validTaskStatus(e.Status) {
		return contracts.InvalidValue("task", "unknown status")
	}
	if (e.ProviderID == "") != (e.ModelID == "") || e.ProviderID == "" && e.ReasoningLevel != "" {
		return contracts.InvalidValue("task.model", "provider and model are required together")
	}
	for _, value := range []string{e.ProviderID, e.ModelID, e.ReasoningLevel} {
		if value != strings.TrimSpace(value) {
			return contracts.InvalidValue("task.model", "model selection must be canonical")
		}
	}
	if e.Status == TaskPending {
		if e.Sequence == 0 ||
			!reflect.ValueOf(e.Input).IsZero() || !e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "" || e.FailureCode != "" {
			return contracts.InvalidValue("task.pending", "pending task requires a sequence and cannot contain execution state")
		}
	} else if err := e.Input.Validate(); err != nil {
		return contracts.FieldError("task.input", err)
	}
	if e.CreatedAt.IsZero() {
		return contracts.InvalidValue("task.createdAt", "creation time is required")
	}
	if (!e.StartedAt.IsZero() && e.StartedAt.Before(e.CreatedAt)) ||
		(!e.EndedAt.IsZero() && e.EndedAt.Before(e.CreatedAt)) {
		return contracts.InvalidValue("task.timestamps", "timestamps cannot precede creation")
	}
	if e.Status == TaskStarting {
		if !e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "" {
			return contracts.InvalidValue("task", "starting task has terminal fields")
		}
	}
	if e.Status == TaskRunning || e.Status == TaskEnding {
		if e.StartedAt.IsZero() || !e.EndedAt.IsZero() || e.Outcome != "" {
			return contracts.InvalidValue("task", "active task has invalid lifecycle fields")
		}
	}
	if e.Status == TaskEnded {
		if e.StartedAt.IsZero() || e.EndedAt.IsZero() || !ValidTaskOutcome(e.Outcome) {
			return contracts.InvalidValue("task", "ended task has invalid terminal fields")
		}
		if e.Outcome == TaskFailed && e.FailureCode == "" {
			return contracts.InvalidValue("task.failureCode", "failed task requires a failure code")
		}
	}
	if !e.FailureCode.Valid() {
		return contracts.InvalidValue("task.failureCode", "unknown failure code")
	}
	if e.FailureCode == contracts.TaskFailureRequestCanceled && e.Outcome != TaskPaused && e.Outcome != TaskInterrupted {
		return contracts.InvalidValue("task.failureCode", "request cancellation requires a paused or interrupted outcome")
	}
	if len([]rune(e.FailureMessage)) > contracts.MaxTaskFailureMessageRunes {
		return contracts.InvalidValue("task.failureMessage", "failure message exceeds length limit")
	}
	if e.Status != TaskEnded && e.FailureMessage != "" {
		return contracts.InvalidValue("task.failureMessage", "active task cannot have a failure message")
	}
	if e.Status == TaskEnded && e.Outcome != TaskFailed && e.FailureMessage != "" {
		return contracts.InvalidValue("task.failureMessage", "only failed task may have a failure message")
	}
	return nil
}

func (e Task) Active() bool {
	return e.Status == TaskStarting || e.Status == TaskRunning || e.Status == TaskEnding
}

func (e *Task) MarkRunning(at time.Time) error {
	if e.Status != TaskStarting {
		return contracts.InvalidTransition("task", string(e.Status), string(TaskRunning))
	}
	e.Status = TaskRunning
	e.StartedAt = at.UTC()
	return nil
}

// BeginEnding 标记执行已返回；结束时间不得早于启动时间，失败时不改变状态。
func (e *Task) BeginEnding(at time.Time) error {
	if e.Status != TaskRunning {
		return contracts.InvalidTransition("task", string(e.Status), string(TaskEnding))
	}
	if at.Before(e.StartedAt) {
		return contracts.InvalidValue("task.endingAt", "ending cannot precede start")
	}
	e.Status = TaskEnding
	return nil
}

// End 固定最终结果和结束时间；已结束的 Task 不允许重新结束或改变结果。
func (e *Task) End(
	outcome TaskOutcome,
	failureCode contracts.TaskFailureCode,
	at time.Time,
) error {
	if e.Status != TaskRunning && e.Status != TaskEnding {
		return contracts.InvalidTransition("task", string(e.Status), string(TaskEnded))
	}
	if !ValidTaskOutcome(outcome) {
		return contracts.InvalidValue("task.outcome", "unknown task outcome")
	}
	if at.Before(e.StartedAt) {
		return contracts.InvalidValue("task.endedAt", "ending cannot precede start")
	}
	if !failureCode.Valid() || (outcome == TaskFailed && failureCode == "") {
		return contracts.InvalidValue("task.failureCode", "unknown or missing failure code")
	}
	if failureCode == contracts.TaskFailureRequestCanceled && outcome != TaskPaused && outcome != TaskInterrupted {
		return contracts.InvalidValue("task.failureCode", "request cancellation requires a paused or interrupted outcome")
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

// ValidTaskOutcome 判断回合结果是否受支持；空值或未知值返回 false。
func ValidTaskOutcome(outcome TaskOutcome) bool {
	switch outcome {
	case TaskCompleted, TaskYielded, TaskPaused, TaskFailed, TaskInterrupted:
		return true
	default:
		return false
	}
}
