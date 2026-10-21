package message

import "praxis/internal/identity"

// StreamKey 是逻辑消息分组，空 OwnerAgentID 表示 Session 共享流。
type StreamKey struct {
	SessionID    identity.SessionID
	OwnerAgentID identity.AgentID
}

// Record 将关联 Agent 与流归属分开；不向 Message 增加持久化字段。
type Record struct {
	Stream  StreamKey
	AgentID identity.AgentID
	Data    Message
}
