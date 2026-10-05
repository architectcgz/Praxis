package repository

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"

	"context"
)

// SessionAgentRepository 负责 Session 内 Agent 运行实例的持久化和查询。
type SessionAgentRepository interface {
	Get(ctx context.Context, id contracts.AgentID) (agentmodel.Agent, error)
	GetBySessionAndDefinition(ctx context.Context, sessionID contracts.SessionID, definitionID contracts.AgentDefinitionID) (agentmodel.Agent, error)
	Save(ctx context.Context, agent agentmodel.Agent) error
	ListBySession(ctx context.Context, sessionID contracts.SessionID, limit int) ([]agentmodel.Agent, error)
}
