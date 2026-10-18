package service

import (
	"context"
	"praxis/internal/core/task"
	"praxis/internal/identity"
)

// Store 只查询和更新预约，所有领取操作必须位于调用方的事务内。
type Store interface {
	Get(context.Context, identity.TaskID) (task.Task, error)
	Save(context.Context, task.Task) error
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (task.Task, error)
	NextSequence(context.Context, identity.AgentID) (uint64, error)
	FindNextPendingByAgent(context.Context, identity.AgentID) (task.Task, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]task.Task, error)
}
