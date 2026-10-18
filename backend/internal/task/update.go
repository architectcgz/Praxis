package task

import (
	"praxis/internal/fault"
	"reflect"
)

// canTransition 是命令方法与持久化更新共同使用的状态转换规则。
func canTransition(from, to TaskStatus) bool {
	if from == to {
		return validTaskStatus(from)
	}
	switch from {
	case TaskPending:
		return to == TaskStarting
	case TaskStarting:
		return to == TaskRunning || to == TaskEnded
	case TaskRunning:
		return to == TaskEnding || to == TaskEnded
	case TaskEnding:
		return to == TaskEnded
	}
	return false
}

// ValidateUpdate 检查不可变身份、冻结配置、生命周期与预算单调性，不递归校验对象。
func ValidateUpdate(before, after Task) error {
	if !canTransition(before.Status, after.Status) {
		return fault.ErrRequestConflict
	}
	if before.Status == TaskEnded && !reflect.DeepEqual(before, after) {
		return fault.ErrRequestConflict
	}
	left, right := before, after
	left.Status, right.Status = "", ""
	left.Progress, right.Progress = Progress{}, Progress{}
	left.StartedAt = right.StartedAt
	left.EndedAt = right.EndedAt
	left.Outcome, right.Outcome = "", ""
	left.FailureCode, right.FailureCode = "", ""
	left.FailureMessage, right.FailureMessage = "", ""
	if before.Status == TaskPending && after.Status == TaskStarting {
		left.Config, right.Config = Config{}, Config{}
		left.ContextSelection, right.ContextSelection = ContextSelection{}, ContextSelection{}
	}
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

// FinalOutput 是调用方已确认的最终输出证据。
type FinalOutput struct {
	AssistantID string
	StopReason  string
}

func (e *Task) RecordFinalOutput(value FinalOutput) error {
	if !e.Active() || e.Progress.FinalAssistantID != "" && (e.Progress.FinalAssistantID != value.AssistantID || e.Progress.FinalStopReason != value.StopReason) {
		return fault.ErrRequestConflict
	}
	e.Progress.FinalAssistantID, e.Progress.FinalStopReason = value.AssistantID, value.StopReason
	return nil
}
