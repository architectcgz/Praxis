package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
)

// MessageRecorder 只写当前 Agent 执行产生的持久化消息（会话历史不回读）。
type MessageRecorder interface {
	Append(context.Context, sessionmodel.MessageData) (sessionmodel.MessageData, error)
}

// MessageRecorderResolver 为一次执行解析其 owner 限定的消息记录器（Session 消息或 Agent 私有消息）。
type MessageRecorderResolver func(context.Context, contracts.SessionID, contracts.AgentID) (MessageRecorder, error)
