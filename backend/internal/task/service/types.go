package service

import (
	"context"

	"praxis/internal/identity"
	"praxis/internal/message"
	"praxis/internal/session"
	"praxis/internal/task"
	"praxis/internal/toolcall"
)

// TaskStore 提供 Task 用例需要的持久化查询能力。
type TaskStore interface {
	task.Store
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (task.Task, error)
	ListActive(context.Context) ([]task.Task, error)
	CountActiveBySession(context.Context, identity.SessionID) (int, error)
	NextSequence(context.Context, identity.AgentID) (uint64, error)
	FindNextPendingByAgent(context.Context, identity.AgentID) (task.Task, error)
}

// SessionStore 提供 Task 上下文冻结和恢复所需的 Session 查询能力。
type SessionStore interface {
	Get(context.Context, identity.SessionID) (session.Session, error)
	Save(context.Context, session.Session) error
	session.ListReader
}

// MessageStreams 提供 Task 输入、历史和工具回执的统一消息能力。
type MessageStreams interface {
	message.Streams
	toolcall.WriterProvider
}

// ExecutionSlots 为 Task 提供进程内执行身份，具体生命周期由 execution 持有。
type ExecutionSlots interface {
	Reserve(context.Context, identity.AgentID, identity.TaskID) (context.Context, bool, error)
	Release(identity.AgentID, identity.TaskID)
	Pause(identity.AgentID, identity.TaskID) bool
	Stop(identity.AgentID, identity.TaskID) bool
}
