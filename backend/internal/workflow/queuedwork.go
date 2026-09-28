package workflow

import (
	"praxis/internal/contracts"
	execution "praxis/internal/execution"

	"strings"
	"time"
	"unicode/utf8"
)

const MaxQueuedWorkPromptBytes = 32 * 1024

// QueuedWorkStatus is the durable lifecycle for an independent task. It is
// intentionally separate from Agent state and never acts as a chat mailbox.
type QueuedWorkStatus string

const (
	QueuedWorkPending     QueuedWorkStatus = "queued"
	QueuedWorkRunning     QueuedWorkStatus = "running"
	QueuedWorkPaused      QueuedWorkStatus = "paused"
	QueuedWorkFailed      QueuedWorkStatus = "failed"
	QueuedWorkInterrupted QueuedWorkStatus = "interrupted"
	QueuedWorkCompleted   QueuedWorkStatus = "completed"
	QueuedWorkCancelled   QueuedWorkStatus = "cancelled"
)

// QueuedWork owns an independent task body. Its execution input is frozen only
// when the task starts so queued time does not hide newer Session context.
type QueuedWork struct {
	ID          contracts.WorkItemID
	SessionID   contracts.SessionID
	AgentID     contracts.AgentID
	Sequence    uint64
	Prompt      string
	Status      QueuedWorkStatus
	ExecutionID contracts.AgentExecutionID
	CreatedAt   time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	FailureCode contracts.ExecutionFailureCode
}

func NewQueuedWork(
	id contracts.WorkItemID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	sequence uint64,
	prompt string,
	at time.Time,
) (QueuedWork, error) {
	work := QueuedWork{
		ID:        id,
		SessionID: sessionID,
		AgentID:   agentID,
		Sequence:  sequence,
		Prompt:    strings.TrimSpace(prompt),
		Status:    QueuedWorkPending,
		CreatedAt: at.UTC(),
	}
	if err := work.Validate(); err != nil {
		return QueuedWork{}, err
	}
	return work, nil
}

func (w QueuedWork) Validate() error {
	if contracts.EmptyID(string(w.ID)) || contracts.EmptyID(string(w.SessionID)) || contracts.EmptyID(string(w.AgentID)) {
		return contracts.InvalidValue("queuedWork", "required reference is missing")
	}
	if w.Sequence == 0 {
		return contracts.InvalidValue("queuedWork.sequence", "sequence must be positive")
	}
	if w.Prompt == "" || len([]byte(w.Prompt)) > MaxQueuedWorkPromptBytes || !utf8.ValidString(w.Prompt) {
		return contracts.InvalidValue("queuedWork.prompt", "prompt is empty, invalid UTF-8, or exceeds the bounded limit")
	}
	if !validQueuedWorkStatus(w.Status) {
		return contracts.InvalidValue("queuedWork.status", "unknown status")
	}
	if w.CreatedAt.IsZero() || (!w.StartedAt.IsZero() && w.StartedAt.Before(w.CreatedAt)) ||
		(!w.FinishedAt.IsZero() && w.FinishedAt.Before(w.CreatedAt)) {
		return contracts.InvalidValue("queuedWork.timestamps", "timestamps are invalid")
	}
	if !w.FailureCode.Valid() {
		return contracts.InvalidValue("queuedWork.failureCode", "unknown failure code")
	}
	switch w.Status {
	case QueuedWorkPending:
		if w.ExecutionID != "" || !w.StartedAt.IsZero() || !w.FinishedAt.IsZero() || w.FailureCode != "" {
			return contracts.InvalidValue("queuedWork.queued", "queued work cannot have execution or settlement fields")
		}
	case QueuedWorkRunning:
		if contracts.EmptyID(string(w.ExecutionID)) || w.StartedAt.IsZero() || !w.FinishedAt.IsZero() || w.FailureCode != "" {
			return contracts.InvalidValue("queuedWork.running", "running work must have only an active execution")
		}
	case QueuedWorkPaused, QueuedWorkInterrupted, QueuedWorkCompleted:
		if contracts.EmptyID(string(w.ExecutionID)) || w.StartedAt.IsZero() || w.FinishedAt.IsZero() {
			return contracts.InvalidValue("queuedWork.settled", "settled work must have execution and timestamps")
		}
	case QueuedWorkFailed:
		if contracts.EmptyID(string(w.ExecutionID)) || w.StartedAt.IsZero() || w.FinishedAt.IsZero() || w.FailureCode == "" {
			return contracts.InvalidValue("queuedWork.failed", "failed work requires execution, timestamps, and failure code")
		}
	case QueuedWorkCancelled:
		if w.ExecutionID != "" || !w.StartedAt.IsZero() || w.FinishedAt.IsZero() || w.FailureCode != "" {
			return contracts.InvalidValue("queuedWork.cancelled", "cancelled work cannot have an execution")
		}
	}
	return nil
}

// Start binds the selected task to exactly one durable execution before any
// runtime activation can occur.
func (w *QueuedWork) Start(executionID contracts.AgentExecutionID, at time.Time) error {
	if w.Status != QueuedWorkPending {
		return contracts.InvalidTransition("queuedWork", string(w.Status), string(QueuedWorkRunning))
	}
	if contracts.EmptyID(string(executionID)) || at.Before(w.CreatedAt) {
		return contracts.InvalidValue("queuedWork.start", "execution id or start time is invalid")
	}
	w.Status = QueuedWorkRunning
	w.ExecutionID = executionID
	w.StartedAt = at.UTC()
	return nil
}

// Settle records the execution's terminal result without changing transcript
// ownership. Interrupted work requires an explicit retry policy or a new task.
func (w *QueuedWork) Settle(
	executionID contracts.AgentExecutionID,
	outcome execution.ExecutionOutcome,
	failureCode contracts.ExecutionFailureCode,
	at time.Time,
) error {
	if w.Status != QueuedWorkRunning || w.ExecutionID != executionID {
		return contracts.InvalidTransition("queuedWork", string(w.Status), "settled")
	}
	if !execution.ValidExecutionOutcome(outcome) || at.Before(w.StartedAt) {
		return contracts.InvalidValue("queuedWork.settlement", "outcome or settlement time is invalid")
	}
	failureCode = contracts.ExecutionFailureCode(strings.TrimSpace(string(failureCode)))
	if !failureCode.Valid() {
		return contracts.InvalidValue("queuedWork.failureCode", "unknown failure code")
	}
	if outcome == execution.ExecutionFailed && failureCode == "" {
		return contracts.InvalidValue("queuedWork.failureCode", "failed work requires a failure code")
	}
	w.Status = queuedWorkStatusForOutcome(outcome)
	w.FinishedAt = at.UTC()
	w.FailureCode = failureCode
	return nil
}

func (w *QueuedWork) Cancel(at time.Time) error {
	if w.Status != QueuedWorkPending || at.Before(w.CreatedAt) {
		return contracts.InvalidTransition("queuedWork", string(w.Status), string(QueuedWorkCancelled))
	}
	w.Status = QueuedWorkCancelled
	w.FinishedAt = at.UTC()
	return nil
}

func validQueuedWorkStatus(status QueuedWorkStatus) bool {
	switch status {
	case QueuedWorkPending, QueuedWorkRunning, QueuedWorkPaused, QueuedWorkFailed,
		QueuedWorkInterrupted, QueuedWorkCompleted, QueuedWorkCancelled:
		return true
	default:
		return false
	}
}

func queuedWorkStatusForOutcome(outcome execution.ExecutionOutcome) QueuedWorkStatus {
	switch outcome {
	case execution.ExecutionCompleted, execution.ExecutionYielded:
		return QueuedWorkCompleted
	case execution.ExecutionPaused:
		return QueuedWorkPaused
	case execution.ExecutionFailed:
		return QueuedWorkFailed
	default:
		return QueuedWorkInterrupted
	}
}
