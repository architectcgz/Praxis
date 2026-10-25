package repository

import (
	"context"
	"praxis/internal/identity"
)

// MissingToolResult 是已声明但尚无回执的工具调用查询结果，不包含恢复决策。
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

// ToolCallQueries 从内存消息声明与回执派生恢复查询，不保存独立工具调用实体。
type ToolCallQueries interface {
	ListMissingToolResults(context.Context) ([]MissingToolResult, error)
}
