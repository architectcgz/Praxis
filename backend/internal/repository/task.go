package repository

import (
	"context"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
)

// Task 查询和更新预约，领取操作必须位于调用方的事务中。
type Task interface {
	Get(context.Context, identity.TaskID) (taskmodel.Task, error)
	Save(context.Context, taskmodel.Task) error
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (taskmodel.Task, error)
	NextSequence(context.Context, identity.AgentID) (uint64, error)
	FindNextPendingByAgent(context.Context, identity.AgentID) (taskmodel.Task, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]taskmodel.Task, error)
}
