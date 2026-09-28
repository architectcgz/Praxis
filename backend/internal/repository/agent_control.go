package repository

import (
	"praxis/internal/contracts"
	workflowmodel "praxis/internal/workflow"

	"context"
)

// AgentControlCommandRepository 负责 Agent 控制命令的持久化和未完成命令查询。
type AgentControlCommandRepository interface {
	Get(ctx context.Context, id contracts.AgentControlCommandID) (workflowmodel.AgentControlCommand, error)
	Save(ctx context.Context, command workflowmodel.AgentControlCommand) error
	ListOpenByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]workflowmodel.AgentControlCommand, error)
}
