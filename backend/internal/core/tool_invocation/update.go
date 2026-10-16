package toolinvocation

import (
	"bytes"
	"praxis/internal/contracts"
	"reflect"
)

func canTransition(from, to ToolInvocationStatus) bool {
	if from == to {
		return from.Valid()
	}
	switch from {
	case ToolInvocationRequested:
		return to == ToolInvocationAwaitingApproval || to == ToolInvocationApproved || to == ToolInvocationDenied || to == ToolInvocationInterrupted
	case ToolInvocationAwaitingApproval:
		return to == ToolInvocationApproved || to == ToolInvocationDenied || to == ToolInvocationInterrupted
	case ToolInvocationApproved:
		return to == ToolInvocationRunning || to == ToolInvocationInterrupted
	case ToolInvocationRunning:
		return to == ToolInvocationSucceeded || to == ToolInvocationFailed || to == ToolInvocationInterrupted || to == ToolInvocationUnknown
	}
	return false
}

// ValidateUpdate 与领域命令共享转换规则，冻结原始调用和已经准入的参数。
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
	if changed && (before.Status != ToolInvocationRequested || len(before.NormalizedArguments) != 0 || after.Status != ToolInvocationApproved && after.Status != ToolInvocationAwaitingApproval && after.Status != ToolInvocationDenied) {
		return contracts.ErrRequestConflict
	}
	if !before.ApprovedAt.IsZero() && (before.Approval != after.Approval || !before.ApprovedAt.Equal(after.ApprovedAt)) || !before.StartedAt.IsZero() && !before.StartedAt.Equal(after.StartedAt) || before.Status != ToolInvocationRequested && before.CommandDecision != after.CommandDecision {
		return contracts.ErrRequestConflict
	}
	return nil
}
