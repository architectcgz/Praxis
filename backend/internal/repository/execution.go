package repository

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
)

// AgentExecutionRepository 负责 Agent 执行记录的持久化和状态查询。
type AgentExecutionRepository interface {
	Get(ctx context.Context, id contracts.AgentExecutionID) (executionmodel.AgentExecution, error)
	Save(ctx context.Context, execution executionmodel.AgentExecution) error
	FindByRequest(
		ctx context.Context,
		agentID contracts.AgentID,
		requestID contracts.RequestID,
	) (executionmodel.AgentExecution, error)
	GetActiveByAgent(ctx context.Context, agentID contracts.AgentID) (executionmodel.AgentExecution, error)
	ListByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]executionmodel.AgentExecution, error)
	CountActiveBySession(ctx context.Context, sessionID contracts.SessionID) (int, error)
}

// ExecutionSecuritySnapshotRepository 提供执行时不可变安全快照的查询。
type ExecutionSecuritySnapshotRepository interface {
	Get(ctx context.Context, executionID contracts.AgentExecutionID) (contracts.ExecutionSecuritySnapshot, error)
}
