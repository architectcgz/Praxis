package message

import (
	"context"
	"praxis/internal/identity"
)

// Writer 提供工具回执的原子消息写入能力。
type Writer interface {
	AppendToolResult(context.Context, Message) error
}

// MissingToolResult 是已声明但尚无回执的工具调用事实，不包含恢复决策。
type MissingToolResult struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	CallID      string
	RequestID   string
	TurnID      identity.TurnID
	Name        string
	Sequence    uint64
	CallIndex   int
}

// Streams 按持久化 Agent 归属选择消息流，调用方不决定日志键位。
type Streams interface {
	Reader
	ListMissingToolResults(context.Context) ([]MissingToolResult, error)
	Append(context.Context, identity.SessionID, identity.AgentID, Message) (Message, error)
	List(context.Context, identity.SessionID, identity.AgentID, uint64, int) ([]Message, error)
}
