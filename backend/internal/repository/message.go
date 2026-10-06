package repository

import (
	"context"

	"praxis/internal/contracts"
	"praxis/internal/core/agent"
	"praxis/internal/core/session"
)

// MessageStream 是单个 Session 或 Agent 消息流的一致读取结果。
type MessageStream struct {
	SequenceBoundary uint64
	Messages         []session.MessageData
}

// MessageLoader 按执行主体可见范围读取消息及其序号边界；读取失败时返回错误。
type MessageLoader interface {
	LoadMessages(context.Context, contracts.SessionID, contracts.AgentID, int) (MessageStream, error)
}

// SessionMessageRepository 按 Session 归属保存和分页读取主 Agent 消息。
type SessionMessageRepository interface {
	Append(context.Context, session.SessionMessage) (session.SessionMessage, error)
	List(context.Context, contracts.SessionID, uint64, int) ([]session.SessionMessage, error)
	LatestSequence(context.Context, contracts.SessionID) (uint64, error)
}

// AgentMessageRepository 按 Agent 归属保存和分页读取协作 Agent 消息。
type AgentMessageRepository interface {
	Append(context.Context, agent.AgentMessage) (agent.AgentMessage, error)
	List(context.Context, contracts.AgentID, uint64, int) ([]agent.AgentMessage, error)
	LatestSequence(context.Context, contracts.AgentID) (uint64, error)
}
