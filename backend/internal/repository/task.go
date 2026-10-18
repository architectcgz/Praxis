package repository

import (
	taskmodel "praxis/internal/core/task"

	"context"
	"praxis/internal/core/identity"
)

// TaskRepository 负责 Agent 执行记录的持久化和状态查询。
type TaskRepository interface {
	Get(ctx context.Context, id identity.TaskID) (taskmodel.Task, error)
	Save(ctx context.Context, task taskmodel.Task) error
	FindByRequest(
		ctx context.Context,
		agentID identity.AgentID,
		requestID identity.RequestID,
	) (taskmodel.Task, error)
	GetActiveByAgent(ctx context.Context, agentID identity.AgentID) (taskmodel.Task, error)
	ListActive(ctx context.Context) ([]taskmodel.Task, error)
	ListByAgent(ctx context.Context, agentID identity.AgentID, limit int) ([]taskmodel.Task, error)
	CountActiveBySession(ctx context.Context, sessionID identity.SessionID) (int, error)
	// NextSequence 在提交事务内分配 Agent 的下一个 FIFO 序号。
	NextSequence(ctx context.Context, agentID identity.AgentID) (uint64, error)
	// FindNextPendingByAgent 读取队首 Task，不改变其状态；空队列返回 ErrNotFound。
	FindNextPendingByAgent(ctx context.Context, agentID identity.AgentID) (taskmodel.Task, error)
}
