package workflow

import (
	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
	"time"
)

type WaitConditionKind string

const (
	WaitForDelivery WaitConditionKind = "delivery"
	WaitForApproval WaitConditionKind = "approval"
	WaitForChild    WaitConditionKind = "child"
)

type WaitConditionMode string

const (
	WaitAny WaitConditionMode = "any"
	WaitAll WaitConditionMode = "all"
)

type WaitConditionStatus string

const (
	WaitPending   WaitConditionStatus = "pending"
	WaitResolved  WaitConditionStatus = "resolved"
	WaitCancelled WaitConditionStatus = "cancelled"
)

type WaitCondition struct {
	ID                identity.WaitConditionID
	AgentID           identity.AgentID
	TaskID            identity.TaskID
	Kind              WaitConditionKind
	Mode              WaitConditionMode
	TargetIDs         []string
	ResolvedTargetIDs []string
	Status            WaitConditionStatus
	CreatedAt         time.Time
	ResolvedAt        time.Time
}

func (w *WaitCondition) Cancel(at time.Time) error {
	if w.Status != WaitPending {
		return fault.InvalidTransition("waitCondition", string(w.Status), string(WaitCancelled))
	}
	w.Status = WaitCancelled
	w.ResolvedAt = at.UTC()
	return nil
}
