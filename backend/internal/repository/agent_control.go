package repository

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"

	"context"
)

// AgentControlCommandRepository 负责 Agent 控制命令的持久化和未完成命令查询。
type AgentControlCommandRepository interface {
	Get(ctx context.Context, id contracts.AgentControlCommandID) (agentmodel.AgentControlCommand, error)
	Save(ctx context.Context, command agentmodel.AgentControlCommand) error
	ListOpenByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]agentmodel.AgentControlCommand, error)
}
