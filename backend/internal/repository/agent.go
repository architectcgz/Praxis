package repository

import (
	agentmodel "praxis/internal/core/agent"

	"context"
	"praxis/internal/core/identity"
)

// SessionAgentRepository 负责 Session 内 Agent 运行实例的持久化和查询。
type SessionAgentRepository interface {
	Get(ctx context.Context, id identity.AgentID) (agentmodel.Agent, error)
	GetBySessionAndDefinition(ctx context.Context, sessionID identity.SessionID, definitionID identity.AgentDefinitionID) (agentmodel.Agent, error)
	Save(ctx context.Context, agent agentmodel.Agent) error
	ListBySession(ctx context.Context, sessionID identity.SessionID, limit int) ([]agentmodel.Agent, error)
}
