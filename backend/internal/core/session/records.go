package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// ExecutionRecords 提供会话持有的执行事实及其耗时记录，不创建运行实例。
type ExecutionRecords interface {
	GetExecution(context.Context, identity.SessionID, identity.AgentExecutionID) (agentexecution.ExecutionRecord, error)
	SaveExecution(context.Context, agentexecution.ExecutionRecord) error
	SaveModelRequest(context.Context, agentexecution.ExecutionRecord, agentexecution.ModelRequestRecord) error
	CreateSubagent(context.Context, agentexecution.ExecutionRecord, sessionentry.ContextEntry) (*sessionentry.ContextEntry, error)
	FindExecutionByRequest(context.Context, identity.AgentID, identity.RequestID) (agentexecution.ExecutionRecord, error)
	ListAgentExecutions(context.Context, identity.SessionID, identity.AgentID, identity.BranchID, int) ([]agentexecution.ExecutionRecord, error)
	ListSessionExecutions(context.Context, identity.SessionID, identity.BranchID) ([]agentexecution.ExecutionRecord, error)
	ListAgentTimingRecords(context.Context, identity.SessionID, identity.AgentID, identity.BranchID, int) ([]timing.Record, error)
}
