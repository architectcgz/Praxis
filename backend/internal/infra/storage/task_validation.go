package storage

import (
	"praxis/internal/core/task"
	"praxis/internal/fault"
	"strings"
)

// validateQueuedTask 在磁盘恢复边界拒绝混入执行状态或不完整领取关系的预约。
func validateQueuedTask(t task.Task) error {
	for _, id := range []string{t.ID.String(), t.SessionID.String(), t.AgentID.String(), t.RequestID.String(), t.InputMessageID} {
		if id == "" || id != strings.TrimSpace(id) {
			return fault.InvalidValue("task", "canonical identity is required")
		}
	}
	if t.Sequence == 0 || t.CreatedAt.IsZero() {
		return fault.InvalidValue("task", "sequence and creation time are required")
	}
	if (t.ProviderID == "") != (t.ModelID == "") || t.ProviderID == "" && t.ReasoningLevel != "" {
		return fault.InvalidValue("task.model", "provider and model are required together")
	}
	for _, v := range []string{t.ProviderID, t.ModelID, t.ReasoningLevel, t.ExecutionID.String()} {
		if v != strings.TrimSpace(v) {
			return fault.InvalidValue("task", "non-canonical value")
		}
	}
	switch t.Status {
	case task.Pending:
		if t.ExecutionID != "" || !t.DispatchedAt.IsZero() || !t.CancelledAt.IsZero() {
			return fault.ErrRequestConflict
		}
	case task.Dispatched:
		if t.ExecutionID == "" || t.DispatchedAt.IsZero() || t.DispatchedAt.Before(t.CreatedAt) || !t.CancelledAt.IsZero() {
			return fault.ErrRequestConflict
		}
	case task.Cancelled:
		if t.ExecutionID != "" || !t.DispatchedAt.IsZero() || t.CancelledAt.IsZero() || t.CancelledAt.Before(t.CreatedAt) {
			return fault.ErrRequestConflict
		}
	default:
		return fault.InvalidValue("task.status", "unknown queue status")
	}
	return nil
}
