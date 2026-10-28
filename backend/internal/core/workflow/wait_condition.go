package workflow

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"praxis/internal/errorcode"
	"praxis/internal/identity"
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

// Validate 检查对象自身的完整性，不修改或修复输入。
func (w WaitCondition) Validate() error {
	for _, id := range []string{w.ID.String(), w.AgentID.String(), w.ExecutionID.String()} {
		if id == "" || id != strings.TrimSpace(id) {
			return fmt.Errorf("waitCondition: required reference must be canonical and nonempty: %w", errorcode.ErrInvalidValue)
		}
	}
	if !slices.Contains([]WaitConditionKind{WaitForDelivery, WaitForApproval, WaitForChild}, w.Kind) ||
		w.Mode != WaitAny && w.Mode != WaitAll ||
		!slices.Contains([]WaitConditionStatus{WaitPending, WaitResolved, WaitCancelled}, w.Status) {
		return fmt.Errorf("waitCondition: unknown kind, mode or status: %w", errorcode.ErrInvalidValue)
	}
	if len(w.TargetIDs) == 0 {
		return fmt.Errorf("waitCondition.targetIDs: at least one target is required: %w", errorcode.ErrInvalidValue)
	}
	seenTargets := make(map[string]bool, len(w.TargetIDs))
	for _, id := range w.TargetIDs {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") || seenTargets[id] {
			return fmt.Errorf("waitCondition.targetIDs: target is invalid or duplicated: %w", errorcode.ErrInvalidValue)
		}
		seenTargets[id] = true
	}
	seenResolved := make(map[string]bool, len(w.ResolvedTargetIDs))
	for _, id := range w.ResolvedTargetIDs {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") || !seenTargets[id] || seenResolved[id] {
			return fmt.Errorf("waitCondition.resolvedTargetIDs: resolved target is invalid, unknown or duplicated: %w", errorcode.ErrInvalidValue)
		}
		seenResolved[id] = true
	}
	if w.CreatedAt.IsZero() || !w.ResolvedAt.IsZero() && w.ResolvedAt.Before(w.CreatedAt) {
		return fmt.Errorf("waitCondition.timestamps: timestamps are invalid: %w", errorcode.ErrInvalidValue)
	}
	if (w.Status == WaitPending) != w.ResolvedAt.IsZero() {
		return fmt.Errorf("waitCondition.resolvedAt: resolution time does not match status: %w", errorcode.ErrInvalidValue)
	}
	if w.Status == WaitResolved && (w.Mode == WaitAny && len(w.ResolvedTargetIDs) == 0 || w.Mode == WaitAll && len(w.ResolvedTargetIDs) != len(w.TargetIDs)) {
		return fmt.Errorf("waitCondition.resolvedTargetIDs: resolved targets do not satisfy the wait: %w", errorcode.ErrInvalidValue)
	}
	return nil
}
