package session

import (
	"context"
	"praxis/internal/identity"
	"time"
)

const (
	EventBranchCreated  = "branch.created"
	EventExecutionSaved = "agent_execution.saved"
	EventEntryAppended  = "entry.appended"
	EventUsageSaved     = "usage.saved"
	EventTimingStarted  = "agent_execution.timing_started"
	EventTimingFinished = "agent_execution.timing_finished"
)

// Event 是会话提交和恢复使用的业务记录，不是进程内通知。
// Payload 按 Type 携带对应的 Branch、ExecutionRecord、ContextEntry、UsageRecord 或 timing.Record 值。
type Event struct {
	Type      string
	Key       string
	BranchID  identity.BranchID
	CreatedAt time.Time
	Payload   any
}

// EventLog 同步追加业务记录并按顺序读取；会话服务负责事件编解码，存储只处理完整行。
// 同一会话由 Session 串行追加；返回成功不承诺断电持久化。
type EventLog interface {
	Read(context.Context, identity.SessionID, func(Event) error) error
	AppendEvent(context.Context, identity.SessionID, Event) error
}
