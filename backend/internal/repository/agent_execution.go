package repository

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/identity"
)

// AgentExecution 读写执行事实；准入、预算预留和结算由 Service 编排。
type AgentExecution interface {
	AppendTiming(context.Context, timing.Record) error
	ListTimings(context.Context, identity.AgentExecutionID) ([]timing.Record, error)
	ListAgentTimings(context.Context, identity.AgentID, int) ([]timing.Record, error)
	Get(context.Context, identity.AgentExecutionID) (agentexecution.ExecutionRecord, error)
	Save(context.Context, agentexecution.ExecutionRecord) error
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (agentexecution.ExecutionRecord, error)
	GetActiveByAgent(context.Context, identity.AgentID) (agentexecution.ExecutionRecord, error)
	ListActive(context.Context) ([]agentexecution.ExecutionRecord, error)
	ListBySession(context.Context, identity.SessionID) ([]agentexecution.ExecutionRecord, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]agentexecution.ExecutionRecord, error)
}
