package hook

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
)

// EntryCommitted 只携带首次保存成功的条目，不允许处理器改变已经提交的事实。
// 通知在业务锁外执行，不保证跨重启必达或并发提交的全局顺序。
type EntryCommitted struct {
	Identity
	Entry sessionentry.ContextEntry
}

func (e EntryCommitted) clone() EntryCommitted {
	e.Entry = e.Entry.Clone()
	return e
}

type UserInputCommittedHook interface {
	UserInputCommitted(context.Context, EntryCommitted) error
}

type AssistantCommittedHook interface {
	AssistantCommitted(context.Context, EntryCommitted) error
}

type FinalOutputCommittedHook interface {
	FinalOutputCommitted(context.Context, EntryCommitted) error
}

// ExecutionEvent 只暴露已经确定的执行事实，不提供可变业务实体或进度写入能力。
type ExecutionEvent struct {
	Identity
	Outcome     string
	FailureCode string
	// ResumeReason 区分进程恢复和审批唤醒；新执行为空。
	ResumeReason string
}

type ExecutionStartedHook interface {
	ExecutionStarted(context.Context, ExecutionEvent) error
}

type ExecutionResumedHook interface {
	ExecutionResumed(context.Context, ExecutionEvent) error
}

type ExecutionPausedHook interface {
	ExecutionPaused(context.Context, ExecutionEvent) error
}

type ExecutionCommittedHook interface {
	ExecutionCommitted(context.Context, ExecutionEvent) error
}
