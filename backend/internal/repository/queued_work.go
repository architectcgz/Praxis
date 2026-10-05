package repository

import (
	"praxis/internal/contracts"
	workflowmodel "praxis/internal/core/workflow"

	"context"
)

// QueuedWorkRepository 负责 Agent 后续用户输入队列的持久化。
type QueuedWorkRepository interface {
	Get(ctx context.Context, id contracts.WorkItemID) (workflowmodel.QueuedWork, error)
	FindByRequest(ctx context.Context, agentID contracts.AgentID, requestID contracts.RequestID) (workflowmodel.QueuedWork, error)
	Save(ctx context.Context, work workflowmodel.QueuedWork) error
	NextSequence(ctx context.Context, agentID contracts.AgentID) (uint64, error)
	FindNextPendingByAgent(ctx context.Context, agentID contracts.AgentID) (workflowmodel.QueuedWork, error)
}
