package repository

import (
	agentmodel "praxis/internal/core/agent"

	"context"
	"praxis/internal/core/identity"
)

// AgentControlCommandRepository 负责 Agent 控制命令的持久化和未完成命令查询。
type AgentControlCommandRepository interface {
	Get(ctx context.Context, id identity.AgentControlCommandID) (agentmodel.AgentControlCommand, error)
	Save(ctx context.Context, command agentmodel.AgentControlCommand) error
	ListOpenByAgent(ctx context.Context, agentID identity.AgentID, limit int) ([]agentmodel.AgentControlCommand, error)
}
