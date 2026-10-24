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
	Get(context.Context, identity.AgentExecutionID) (agentexecution.Execution, error)
	Save(context.Context, agentexecution.Execution) error
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (agentexecution.Execution, error)
	GetActiveByAgent(context.Context, identity.AgentID) (agentexecution.Execution, error)
	ListActive(context.Context) ([]agentexecution.Execution, error)
	ListBySession(context.Context, identity.SessionID) ([]agentexecution.Execution, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]agentexecution.Execution, error)
}
