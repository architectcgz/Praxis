package task

import (
	"context"
	"praxis/internal/identity"
)

// Store 只查询和更新预约，所有领取操作必须位于调用方的事务内。
type Store interface {
	Get(context.Context, identity.TaskID) (Task, error)
	Save(context.Context, Task) error
	FindByRequest(context.Context, identity.AgentID, identity.RequestID) (Task, error)
	NextSequence(context.Context, identity.AgentID) (uint64, error)
	FindNextPendingByAgent(context.Context, identity.AgentID) (Task, error)
	ListByAgent(context.Context, identity.AgentID, int) ([]Task, error)
}
