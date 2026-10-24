package repository

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
)

// Agent 提供会话内 Agent 实例的持久化和查询能力。
type Agent interface {
	Get(context.Context, identity.AgentID) (agentmodel.Agent, error)
	GetMainBySession(context.Context, identity.SessionID) (agentmodel.Agent, error)
	Save(context.Context, agentmodel.Agent) error
	ListBySession(context.Context, identity.SessionID, int) ([]agentmodel.Agent, error)
}
