package repository

import (
	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"

	"context"
)

// TaskRepository 负责 Agent 执行记录的持久化和状态查询。
type TaskRepository interface {
	Get(ctx context.Context, id contracts.TaskID) (taskmodel.Task, error)
	Save(ctx context.Context, task taskmodel.Task) error
	FindByRequest(
		ctx context.Context,
		agentID contracts.AgentID,
		requestID contracts.RequestID,
	) (taskmodel.Task, error)
	GetActiveByAgent(ctx context.Context, agentID contracts.AgentID) (taskmodel.Task, error)
	ListActive(ctx context.Context) ([]taskmodel.Task, error)
	ListByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]taskmodel.Task, error)
	CountActiveBySession(ctx context.Context, sessionID contracts.SessionID) (int, error)
	// NextSequence 在提交事务内分配 Agent 的下一个 FIFO 序号。
	NextSequence(ctx context.Context, agentID contracts.AgentID) (uint64, error)
	// FindNextPendingByAgent 读取队首 Task，不改变其状态；空队列返回 ErrNotFound。
	FindNextPendingByAgent(ctx context.Context, agentID contracts.AgentID) (taskmodel.Task, error)
}

// SecuritySnapshotRepository 提供执行时不可变安全快照的查询。
type SecuritySnapshotRepository interface {
	Get(ctx context.Context, taskID contracts.TaskID) (contracts.SecuritySnapshot, error)
}
