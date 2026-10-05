package repository

import (
	"praxis/internal/contracts"
	workflowmodel "praxis/internal/core/workflow"

	"context"
)

// WaitConditionRepository 负责等待条件的持久化和未决条件查询。
type WaitConditionRepository interface {
	Get(ctx context.Context, id contracts.WaitConditionID) (workflowmodel.WaitCondition, error)
	Save(ctx context.Context, condition workflowmodel.WaitCondition) error
	ListUnresolvedByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]workflowmodel.WaitCondition, error)
}
