package message

import (
	"context"
	"praxis/internal/identity"
)

// Stream 是一次一致性读取返回的消息集合和序号边界。
type Stream struct {
	SequenceBoundary uint64
	Messages         []Message
}

// Reader 按 Session 和 Agent 的消息流归属读取消息。
type Reader interface {
	LoadMessages(context.Context, identity.SessionID, identity.AgentID, int) (Stream, error)
}
