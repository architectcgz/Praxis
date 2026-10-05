package workflow

import (
	"praxis/internal/contracts"
	turn "praxis/internal/core/turn"

	"strings"
	"time"
)

const MaxQueuedWorkPromptBytes = 32 * 1024

// QueuedWorkStatus 表示用户输入等待 Agent 执行期间的持久化队列状态。
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

// QueuedWork 保存 Agent 忙碌期间提交的用户输入消息引用和 FIFO 状态。
// 它表示用户输入排队，不表示 Agent 之间的任务交接。
type QueuedWork struct {
	ID             contracts.WorkItemID
	SessionID      contracts.SessionID
	AgentID        contracts.AgentID
	Sequence       uint64
	RequestID      contracts.RequestID
	InputMessageID string
	Status         QueuedWorkStatus
	TurnID         contracts.TurnID
	CreatedAt      time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
	FailureCode    contracts.TurnFailureCode
}

// NewQueuedWork 创建关联到已持久化输入消息的待执行队列记录。
func NewQueuedWork(
	id contracts.WorkItemID,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	requestID contracts.RequestID,
	inputMessageID string,
	sequence uint64,
	at time.Time,
) (QueuedWork, error) {
	work := QueuedWork{
		ID:             id,
		SessionID:      sessionID,
		AgentID:        agentID,
		Sequence:       sequence,
		RequestID:      requestID,
		InputMessageID: inputMessageID,
		Status:         QueuedWorkPending,
		CreatedAt:      at.UTC(),
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
	if w.RequestID == "" || w.InputMessageID == "" || w.InputMessageID != strings.TrimSpace(w.InputMessageID) {
		return contracts.InvalidValue("queuedWork.inputMessageID", "request identity and canonical input message are required")
	}
	if w.Sequence == 0 {
		return contracts.InvalidValue("queuedWork.sequence", "sequence must be positive")
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
		if w.TurnID != "" || !w.StartedAt.IsZero() || !w.FinishedAt.IsZero() || w.FailureCode != "" {
			return contracts.InvalidValue("queuedWork.queued", "queued work cannot have turn or settlement fields")
		}
	case QueuedWorkRunning:
		if contracts.EmptyID(string(w.TurnID)) || w.StartedAt.IsZero() || !w.FinishedAt.IsZero() || w.FailureCode != "" {
			return contracts.InvalidValue("queuedWork.running", "running work must have only an active turn")
		}
	case QueuedWorkPaused, QueuedWorkInterrupted, QueuedWorkCompleted:
		if contracts.EmptyID(string(w.TurnID)) || w.StartedAt.IsZero() || w.FinishedAt.IsZero() {
			return contracts.InvalidValue("queuedWork.settled", "settled work must have turn and timestamps")
		}
	case QueuedWorkFailed:
		if contracts.EmptyID(string(w.TurnID)) || w.StartedAt.IsZero() || w.FinishedAt.IsZero() || w.FailureCode == "" {
			return contracts.InvalidValue("queuedWork.failed", "failed work requires turn, timestamps, and failure code")
		}
	case QueuedWorkCancelled:
		if w.TurnID != "" || !w.StartedAt.IsZero() || w.FinishedAt.IsZero() || w.FailureCode != "" {
			return contracts.InvalidValue("queuedWork.cancelled", "cancelled work cannot have an turn")
		}
	}
	return nil
}

// Start 将队列项绑定到唯一的持久化 Turn，成功后才能激活 runtime。
func (w *QueuedWork) Start(turnID contracts.TurnID, at time.Time) error {
	if w.Status != QueuedWorkPending {
		return contracts.InvalidTransition("queuedWork", string(w.Status), string(QueuedWorkRunning))
	}
	if contracts.EmptyID(string(turnID)) || at.Before(w.CreatedAt) {
		return contracts.InvalidValue("queuedWork.start", "turn id or start time is invalid")
	}
	w.Status = QueuedWorkRunning
	w.TurnID = turnID
	w.StartedAt = at.UTC()
	return nil
}

// Settle 记录执行终态而不改变消息归属；中断输入只能按明确策略重试或重新提交。
func (w *QueuedWork) Settle(
	turnID contracts.TurnID,
	outcome turn.TurnOutcome,
	failureCode contracts.TurnFailureCode,
	at time.Time,
) error {
	if w.Status != QueuedWorkRunning || w.TurnID != turnID {
		return contracts.InvalidTransition("queuedWork", string(w.Status), "settled")
	}
	if !turn.ValidTurnOutcome(outcome) || at.Before(w.StartedAt) {
		return contracts.InvalidValue("queuedWork.settlement", "outcome or settlement time is invalid")
	}
	if !failureCode.Valid() {
		return contracts.InvalidValue("queuedWork.failureCode", "unknown failure code")
	}
	if outcome == turn.TurnFailed && failureCode == "" {
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

func queuedWorkStatusForOutcome(outcome turn.TurnOutcome) QueuedWorkStatus {
	switch outcome {
	case turn.TurnCompleted, turn.TurnYielded:
		return QueuedWorkCompleted
	case turn.TurnPaused:
		return QueuedWorkPaused
	case turn.TurnFailed:
		return QueuedWorkFailed
	default:
		return QueuedWorkInterrupted
	}
}
