package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

// ExecutionRecords 提供会话持有的执行事实及其耗时记录，不创建运行实例。
type ExecutionRecords interface {
	GetExecution(context.Context, identity.SessionID, identity.AgentExecutionID) (agentexecution.ExecutionRecord, error)
	SaveExecution(context.Context, agentexecution.ExecutionRecord) error
	FindExecutionByRequest(context.Context, identity.AgentID, identity.RequestID) (agentexecution.ExecutionRecord, error)
	ListAgentExecutions(context.Context, identity.AgentID, identity.BranchID, int) ([]agentexecution.ExecutionRecord, error)
	AppendTiming(context.Context, identity.SessionID, timing.Record) error
	ListAgentTimingRecords(context.Context, identity.AgentID, identity.BranchID, int) ([]timing.Record, error)
}

// UsageRecords 保存并查询会话持有的模型用量事实。
type UsageRecords interface {
	SaveUsage(context.Context, providerapi.UsageRecord) error
	ListSessionUsage(context.Context, identity.SessionID) ([]providerapi.UsageRecord, error)
}
