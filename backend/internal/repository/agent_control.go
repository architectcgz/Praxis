package repository

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
)

// AgentControl 持久化控制命令并查询尚未应用的控制意图。
type AgentControl interface {
	Get(context.Context, identity.AgentControlCommandID) (agentmodel.AgentControlCommand, error)
	Save(context.Context, agentmodel.AgentControlCommand) error
	ListOpenByAgent(context.Context, identity.AgentID, int) ([]agentmodel.AgentControlCommand, error)
}
