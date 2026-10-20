package agent

import (
	"praxis/internal/fault"
	"reflect"
)

// canExecutionTransition 是命令方法与持久化更新共同使用的状态转换规则。
func canExecutionTransition(from, to ExecutionStatus) bool {
	if from == to {
		return validExecutionStatus(from)
	}
	switch from {
	case ExecutionStarting:
		return to == ExecutionRunning || to == ExecutionEnded
	case ExecutionRunning:
		return to == ExecutionWaitingApproval || to == ExecutionEnding || to == ExecutionEnded
	case ExecutionWaitingApproval:
		return to == ExecutionRunning || to == ExecutionEnding || to == ExecutionEnded
	case ExecutionEnding:
		return to == ExecutionEnded
	}
	return false
}

// ValidateExecutionUpdate 检查不可变身份、冻结配置、生命周期与预算单调性，不递归校验对象。
func ValidateExecutionUpdate(before, after Execution) error {
	if !canExecutionTransition(before.Status, after.Status) {
		return fault.ErrRequestConflict
	}
	if before.Status == ExecutionEnded {
		prior := before
		prior.Progress.OutputBytes = after.Progress.OutputBytes
		if !reflect.DeepEqual(prior, after) {
			return fault.ErrRequestConflict
		}
	}
	left, right := before, after
	left.Status, right.Status = "", ""
	left.Progress, right.Progress = ExecutionProgress{}, ExecutionProgress{}
	left.StartedAt = right.StartedAt
	left.EndedAt = right.EndedAt
	left.Outcome, right.Outcome = "", ""
	left.FailureCode, right.FailureCode = "", ""
	left.FailureMessage, right.FailureMessage = "", ""
	if !reflect.DeepEqual(left, right) || !before.StartedAt.IsZero() && !before.StartedAt.Equal(after.StartedAt) {
		return fault.ErrRequestConflict
	}
	p, n := before.Progress, after.Progress
	if n.NextSequence < p.NextSequence || n.Iterations < p.Iterations || n.ModelRequests < p.ModelRequests || n.OutputBytes < p.OutputBytes || n.ToolCalls < p.ToolCalls {
		return fault.ErrRequestConflict
	}
	if p.FinalAssistantID != "" && (p.FinalAssistantID != n.FinalAssistantID || p.FinalStopReason != n.FinalStopReason) {
		return fault.ErrRequestConflict
	}
	return nil
}

func (e *Execution) RecordFinalOutput(value FinalOutput) error {
	if !e.Active() || e.Progress.FinalAssistantID != "" && (e.Progress.FinalAssistantID != value.AssistantID || e.Progress.FinalStopReason != value.StopReason) {
		return fault.ErrRequestConflict
	}
	e.Progress.FinalAssistantID, e.Progress.FinalStopReason = value.AssistantID, value.StopReason
	return nil
}
