package workflow

import (
	"fmt"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
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
	ExecutionID       identity.AgentExecutionID
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
		return fmt.Errorf("waitCondition cannot transition from %q to %q: %w", w.Status, WaitCancelled, errorcode.ErrInvalidTransition)
	}
	w.Status = WaitCancelled
	w.ResolvedAt = at.UTC()
	return nil
}
