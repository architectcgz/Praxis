package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
)

// TurnMessageStore 读写当前 Agent 执行产生的持久化消息。
type TurnMessageStore interface {
	Append(context.Context, sessionmodel.MessageData) (sessionmodel.MessageData, error)
	ListByTurn(context.Context, contracts.TurnID) ([]sessionmodel.MessageData, error)
}

// MessageStoreResolver 为一次执行解析其 owner 限定的消息存储。
type MessageStoreResolver func(context.Context, contracts.SessionID, contracts.AgentID) (TurnMessageStore, error)
