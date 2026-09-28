package repository

import (
	"praxis/internal/contracts"
	workflowmodel "praxis/internal/workflow"

	"context"
)

// QueuedWorkRepository 负责独立任务队列记录的持久化。
type QueuedWorkRepository interface {
	Get(ctx context.Context, id contracts.WorkItemID) (workflowmodel.QueuedWork, error)
	Save(ctx context.Context, work workflowmodel.QueuedWork) error
	NextSequence(ctx context.Context, agentID contracts.AgentID) (uint64, error)
	FindNextPendingByAgent(ctx context.Context, agentID contracts.AgentID) (workflowmodel.QueuedWork, error)
}
