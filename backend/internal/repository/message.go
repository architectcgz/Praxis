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

// MessageStreams 按 Agent 可见范围读写消息流：主 Agent 使用 Session 流，其他 Agent 使用私有流。
// 可见性判定由 core/agent 决定，具体日志键位由存储实现决定，调用方不解释存储布局。
type MessageStreams interface {
	MessageLoader
	// Append 幂等追加消息到该 Agent 的可见流；身份相同但内容不一致时返回冲突。
	Append(context.Context, agent.Agent, session.MessageData) (session.MessageData, error)
	// List 返回该 Agent 可见流中游标之后的记录副本；非正数 limit 表示不限制。
	List(context.Context, agent.Agent, uint64, int) ([]session.MessageData, error)
}
