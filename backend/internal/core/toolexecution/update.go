package toolexecution

import (
	"bytes"
	"praxis/internal/fault"
	"reflect"
)

func canTransition(from, to Status) bool {
	if from == to {
		return from.Valid()
	}
	switch from {
	case StatusRequested:
		return to == StatusPrepared || to == StatusAwaitingApproval || to == StatusApproved || to == StatusDenied || to == StatusInterrupted
	case StatusPrepared:
		return to == StatusReviewing || to == StatusAwaitingApproval || to == StatusApproved || to == StatusDenied || to == StatusInterrupted
	case StatusReviewing:
		return to == StatusPrepared || to == StatusApproved || to == StatusAwaitingApproval || to == StatusDenied || to == StatusInterrupted
	case StatusAwaitingApproval:
		return to == StatusApproved || to == StatusDenied || to == StatusInterrupted
	case StatusApproved:
		return to == StatusPrepared || to == StatusRunning || to == StatusInterrupted || to == StatusAwaitingApproval
	case StatusRunning:
		return to == StatusSucceeded || to == StatusFailed || to == StatusInterrupted || to == StatusUnknown
	}
	return false
}

// ValidateUpdate 检查前后状态的转换与不可变字段；对象本身由输入或状态变更入口检查。
func ValidateUpdate(before, after Execution) error {
	if !canTransition(before.Status, after.Status) || before.Status.Terminal() && !reflect.DeepEqual(before, after) || before.BudgetReserved && !after.BudgetReserved {
		return fault.ErrRequestConflict
	}
	if before.ID != after.ID || before.ExecutionID != after.ExecutionID || before.TurnID != after.TurnID || before.SessionID != after.SessionID || before.AgentID != after.AgentID || before.ProviderToolCallID != after.ProviderToolCallID || before.Name != after.Name || !bytes.Equal(before.Arguments, after.Arguments) || before.ArgumentsDigest != after.ArgumentsDigest || !before.CreatedAt.Equal(after.CreatedAt) {
		return fault.ErrRequestConflict
	}
	changed := !bytes.Equal(before.NormalizedArguments, after.NormalizedArguments) || before.Path != after.Path
	if changed && (before.Status != StatusRequested || len(before.NormalizedArguments) != 0 || after.Status != StatusPrepared && after.Status != StatusApproved && after.Status != StatusAwaitingApproval && after.Status != StatusDenied) {
		return fault.ErrRequestConflict
	}
	approvalRevoked := before.Status == StatusApproved && (after.Status == StatusAwaitingApproval || after.Status == StatusPrepared) && after.Approval == (ApprovalRecord{}) && after.ApprovedAt.IsZero()
	if !approvalRevoked && !before.ApprovedAt.IsZero() && (before.Approval != after.Approval || !before.ApprovedAt.Equal(after.ApprovedAt)) || !before.StartedAt.IsZero() && !before.StartedAt.Equal(after.StartedAt) {
		return fault.ErrRequestConflict
	}
	if before.Decision != after.Decision && before.Status != StatusRequested && before.Status != StatusPrepared && after.Status != StatusPrepared {
		return fault.ErrRequestConflict
	}
	if len(after.Reviews) < len(before.Reviews) || len(after.Reviews) > len(before.Reviews)+1 {
		return fault.ErrRequestConflict
	}
	for index, prior := range before.Reviews {
		current := after.Reviews[index]
		if prior == current {
			continue
		}
		if index != len(before.Reviews)-1 || before.Status != StatusReviewing || !prior.CompletedAt.IsZero() || current.CompletedAt.IsZero() {
			return fault.ErrRequestConflict
		}
		current.CompletedAt = prior.CompletedAt
		current.Assessment = prior.Assessment
		if current != prior {
			return fault.ErrRequestConflict
		}
	}
	if len(after.Reviews) > len(before.Reviews) && (after.Status != StatusReviewing || !after.LatestReview().CompletedAt.IsZero()) {
		return fault.ErrRequestConflict
	}
	return nil
}
