package toolinvocation

import (
	"bytes"
	"reflect"

	"praxis/internal/contracts"
)

func canTransition(from, to ToolInvocationStatus) bool {
	if from == to {
		return from.Valid()
	}
	switch from {
	case ToolInvocationRequested:
		return to == ToolInvocationPrepared || to == ToolInvocationAwaitingApproval || to == ToolInvocationApproved || to == ToolInvocationDenied || to == ToolInvocationInterrupted
	case ToolInvocationPrepared:
		return to == ToolInvocationReviewing || to == ToolInvocationAwaitingApproval || to == ToolInvocationApproved || to == ToolInvocationDenied || to == ToolInvocationInterrupted
	case ToolInvocationReviewing:
		return to == ToolInvocationPrepared || to == ToolInvocationApproved || to == ToolInvocationAwaitingApproval || to == ToolInvocationDenied || to == ToolInvocationInterrupted
	case ToolInvocationAwaitingApproval:
		return to == ToolInvocationApproved || to == ToolInvocationDenied || to == ToolInvocationInterrupted
	case ToolInvocationApproved:
		return to == ToolInvocationPrepared || to == ToolInvocationRunning || to == ToolInvocationInterrupted || to == ToolInvocationAwaitingApproval
	case ToolInvocationRunning:
		return to == ToolInvocationSucceeded || to == ToolInvocationFailed || to == ToolInvocationInterrupted || to == ToolInvocationUnknown
	}
	return false
}

// ValidateUpdate 与领域命令共享转换规则，冻结原始调用、执行参数与已经完成的审核。
func ValidateUpdate(before, after ToolInvocation) error {
	if err := after.Validate(); err != nil {
		return err
	}
	if !canTransition(before.Status, after.Status) || before.Status.Terminal() && !reflect.DeepEqual(before, after) || before.BudgetReserved && !after.BudgetReserved {
		return contracts.ErrRequestConflict
	}
	if before.ID != after.ID || before.TaskID != after.TaskID || before.TurnID != after.TurnID || before.SessionID != after.SessionID || before.AgentID != after.AgentID || before.ProviderToolCallID != after.ProviderToolCallID || before.Name != after.Name || !bytes.Equal(before.Arguments, after.Arguments) || before.ArgumentsDigest != after.ArgumentsDigest || !before.CreatedAt.Equal(after.CreatedAt) {
		return contracts.ErrRequestConflict
	}
	changed := !bytes.Equal(before.NormalizedArguments, after.NormalizedArguments) || before.Path != after.Path
	if changed && (before.Status != ToolInvocationRequested || len(before.NormalizedArguments) != 0 || after.Status != ToolInvocationPrepared && after.Status != ToolInvocationApproved && after.Status != ToolInvocationAwaitingApproval && after.Status != ToolInvocationDenied) {
		return contracts.ErrRequestConflict
	}
	approvalRevoked := before.Status == ToolInvocationApproved && (after.Status == ToolInvocationAwaitingApproval || after.Status == ToolInvocationPrepared) && after.Approval == (contracts.ApprovalRecord{}) && after.ApprovedAt.IsZero()
	if !approvalRevoked && !before.ApprovedAt.IsZero() && (before.Approval != after.Approval || !before.ApprovedAt.Equal(after.ApprovedAt)) || !before.StartedAt.IsZero() && !before.StartedAt.Equal(after.StartedAt) {
		return contracts.ErrRequestConflict
	}
	if before.Decision != after.Decision && before.Status != ToolInvocationRequested && before.Status != ToolInvocationPrepared && after.Status != ToolInvocationPrepared {
		return contracts.ErrRequestConflict
	}
	if len(after.Reviews) < len(before.Reviews) || len(after.Reviews) > len(before.Reviews)+1 {
		return contracts.ErrRequestConflict
	}
	for index, prior := range before.Reviews {
		current := after.Reviews[index]
		if prior == current {
			continue
		}
		if index != len(before.Reviews)-1 || before.Status != ToolInvocationReviewing || !prior.CompletedAt.IsZero() || current.CompletedAt.IsZero() {
			return contracts.ErrRequestConflict
		}
		current.CompletedAt = prior.CompletedAt
		current.Assessment = prior.Assessment
		if current != prior {
			return contracts.ErrRequestConflict
		}
	}
	if len(after.Reviews) > len(before.Reviews) && (after.Status != ToolInvocationReviewing || !after.LatestReview().CompletedAt.IsZero()) {
		return contracts.ErrRequestConflict
	}
	return nil
}
