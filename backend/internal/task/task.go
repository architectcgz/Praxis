package task

import (
	"praxis/internal/fault"
	"praxis/internal/identity"
	"reflect"
	"time"
)

const MaxInputBytes = 32 * 1024

type Status string

const (
	Pending    Status = "pending"
	Dispatched Status = "dispatched"
	Cancelled  Status = "cancelled"
)

// Task 只保存预约输入与 FIFO 归属，执行结果由关联的 Execution 保存。
type Task struct {
	ID             identity.TaskID
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	RequestID      identity.RequestID
	Sequence       uint64
	ProviderID     string
	ModelID        string
	ReasoningLevel string
	InputMessageID string
	Status         Status
	ExecutionID    identity.AgentExecutionID
	CreatedAt      time.Time
	DispatchedAt   time.Time
	CancelledAt    time.Time
}

func NewTask(id identity.TaskID, sessionID identity.SessionID, agentID identity.AgentID, requestID identity.RequestID, inputMessageID string, sequence uint64, at time.Time) Task {
	return Task{
		ID:             id,
		SessionID:      sessionID,
		AgentID:        agentID,
		RequestID:      requestID,
		InputMessageID: inputMessageID,
		Sequence:       sequence,
		Status:         Pending,
		CreatedAt:      at.UTC(),
	}
}

// Prepare 确认预约仍可领取；配置和上下文在领取事务中冻结到 Execution。
func (t Task) Prepare() error {
	if t.Status != Pending {
		return fault.ErrRequestConflict
	}
	return nil
}

func (t *Task) MarkDispatched(id identity.AgentExecutionID, at time.Time) error {
	if t.Status != Pending || id == "" || at.Before(t.CreatedAt) {
		return fault.ErrRequestConflict
	}
	t.Status, t.ExecutionID, t.DispatchedAt = Dispatched, id, at.UTC()
	return nil
}

func (t *Task) Cancel(at time.Time) error {
	if t.Status != Pending || at.Before(t.CreatedAt) {
		return fault.ErrRequestConflict
	}
	t.Status, t.CancelledAt = Cancelled, at.UTC()
	return nil
}

// ValidateUpdate 保证领取与取消只能发生一次，预约内容和队列顺序不可变。
func ValidateUpdate(before, after Task) error {
	if before.Status != Pending {
		if !reflect.DeepEqual(before, after) {
			return fault.ErrRequestConflict
		}
		return nil
	}
	if after.Status != Pending && after.Status != Dispatched && after.Status != Cancelled {
		return fault.ErrRequestConflict
	}
	left := after
	left.Status, left.ExecutionID, left.DispatchedAt, left.CancelledAt = before.Status, before.ExecutionID, before.DispatchedAt, before.CancelledAt
	if !reflect.DeepEqual(before, left) {
		return fault.ErrRequestConflict
	}
	return nil
}
