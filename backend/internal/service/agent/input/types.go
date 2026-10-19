package input

import (
	"context"

	"praxis/internal/core/message"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	agentmodel "praxis/internal/service/agent"
	"praxis/internal/service/session"
)

// ExecutionStore 提供 Execution 用例需要的持久化查询能力。
type ExecutionStore interface {
	agentmodel.ExecutionStore
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (agentmodel.Execution, error)
	ListActive(context.Context) ([]agentmodel.Execution, error)
	CountActiveBySession(context.Context, identity.SessionID) (int, error)
}

// SessionStore 提供 Execution 上下文冻结和恢复所需的 Session 查询能力。
type SessionStore interface {
	Get(context.Context, identity.SessionID) (session.Session, error)
	Save(context.Context, session.Session) error
	session.ListReader
}

// MessageStreams 提供 Execution 输入、历史和工具回执的统一消息能力。
type MessageStreams interface {
	message.Streams
	toolexecution.WriterProvider
}

// ActiveTasks 为 Execution 提供进程内执行身份，具体生命周期由 execution 持有。
type ActiveTasks interface {
	Reserve(context.Context, identity.AgentID, identity.AgentExecutionID) (context.Context, bool, error)
	Release(identity.AgentID, identity.AgentExecutionID)
	Pause(identity.AgentID, identity.AgentExecutionID) bool
	Stop(identity.AgentID, identity.AgentExecutionID) bool
}
