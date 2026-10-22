package repository

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/identity"
)

// AgentExecution 读写执行事实；准入、预算预留和结算由 Service 编排。
type AgentExecution interface {
	AppendTiming(context.Context, timing.Record) error
	ListTimings(context.Context, identity.AgentExecutionID) ([]timing.Record, error)
	ListAgentTimings(context.Context, identity.AgentID, int) ([]timing.Record, error)
	Get(context.Context, identity.AgentExecutionID) (agentmodel.Execution, error)
	Save(context.Context, agentmodel.Execution) error
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (agentmodel.Execution, error)
	GetActiveByAgent(context.Context, identity.AgentID) (agentmodel.Execution, error)
	ListActive(context.Context) ([]agentmodel.Execution, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]agentmodel.Execution, error)
	CountActiveBySession(context.Context, identity.SessionID) (int, error)
}
