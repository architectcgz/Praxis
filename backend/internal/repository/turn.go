package repository

import (
	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"

	"context"
)

// TurnRepository 负责 Agent 执行记录的持久化和状态查询。
type TurnRepository interface {
	Get(ctx context.Context, id contracts.TurnID) (turnmodel.Turn, error)
	Save(ctx context.Context, turn turnmodel.Turn) error
	FindByRequest(
		ctx context.Context,
		agentID contracts.AgentID,
		requestID contracts.RequestID,
	) (turnmodel.Turn, error)
	GetActiveByAgent(ctx context.Context, agentID contracts.AgentID) (turnmodel.Turn, error)
	ListActive(ctx context.Context) ([]turnmodel.Turn, error)
	ListByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]turnmodel.Turn, error)
	CountActiveBySession(ctx context.Context, sessionID contracts.SessionID) (int, error)
}

// SecuritySnapshotRepository 提供执行时不可变安全快照的查询。
type SecuritySnapshotRepository interface {
	Get(ctx context.Context, turnID contracts.TurnID) (contracts.SecuritySnapshot, error)
}
