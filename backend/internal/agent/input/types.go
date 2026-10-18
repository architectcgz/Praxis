package service

import (
	"context"

	taskmodel "praxis/internal/agent"
	"praxis/internal/identity"
	"praxis/internal/message"
	"praxis/internal/session"
	"praxis/internal/toolcall"
)

// TaskStore 提供 Task 用例需要的持久化查询能力。
type TaskStore interface {
	taskmodel.ExecutionStore
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (taskmodel.Execution, error)
	ListActive(context.Context) ([]taskmodel.Execution, error)
	CountActiveBySession(context.Context, identity.SessionID) (int, error)
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

// ActiveTasks 为 Task 提供进程内执行身份，具体生命周期由 execution 持有。
type ActiveTasks interface {
	Reserve(context.Context, identity.AgentID, identity.AgentExecutionID) (context.Context, bool, error)
	Release(identity.AgentID, identity.AgentExecutionID)
	Pause(identity.AgentID, identity.AgentExecutionID) bool
	Stop(identity.AgentID, identity.AgentExecutionID) bool
}
