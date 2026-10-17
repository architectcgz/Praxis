package workflow

import (
	"time"

	"praxis/internal/contracts"
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
	ID                contracts.WaitConditionID
	AgentID           contracts.AgentID
	TaskID            contracts.TaskID
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
		return contracts.InvalidTransition("waitCondition", string(w.Status), string(WaitCancelled))
	}
	w.Status = WaitCancelled
	w.ResolvedAt = at.UTC()
	return nil
}
