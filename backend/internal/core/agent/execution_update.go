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
	left, right := before, after
	left.Revision, right.Revision = 0, 0
	// 派生统计不参与持久化更新校验；终态仅允许补记已返回的模型输出消耗。
	left.Progress, right.Progress = ExecutionProgress{}, ExecutionProgress{}
	if before.Status == ExecutionEnded && !reflect.DeepEqual(left, right) {
		return fault.ErrRequestConflict
	}
	left.Status, right.Status = "", ""
	left.StartedAt = right.StartedAt
	left.EndedAt = right.EndedAt
	left.Outcome, right.Outcome = "", ""
	left.FailureCode, right.FailureCode = "", ""
	left.FailureMessage, right.FailureMessage = "", ""
	if !reflect.DeepEqual(left, right) || !before.StartedAt.IsZero() && !before.StartedAt.Equal(after.StartedAt) {
		return fault.ErrRequestConflict
	}
	p, n := before.Progress, after.Progress
	if n.NextSequence < p.NextSequence || n.Iterations < p.Iterations || n.ModelRequests < p.ModelRequests || n.ModelOutputBytes < p.ModelOutputBytes {
		return fault.ErrRequestConflict
	}
	if before.Status == ExecutionEnded && (n.NextSequence != p.NextSequence || n.Iterations != p.Iterations || n.ModelRequests != p.ModelRequests) {
		return fault.ErrRequestConflict
	}
	return nil
}
