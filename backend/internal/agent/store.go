package agent

import (
	"context"
	"praxis/internal/identity"
)

// Store 提供 Session 内 Agent 运行实例的持久化和查询能力。
type Store interface {
	Get(context.Context, identity.AgentID) (Agent, error)
	GetBySessionAndDefinition(context.Context, identity.SessionID, identity.AgentDefinitionID) (Agent, error)
	Save(context.Context, Agent) error
	ListBySession(context.Context, identity.SessionID, int) ([]Agent, error)
}

// ControlStore 提供 Agent 控制命令的持久化和未完成命令查询能力。
type ControlStore interface {
	Get(context.Context, identity.AgentControlCommandID) (AgentControlCommand, error)
	Save(context.Context, AgentControlCommand) error
	ListOpenByAgent(context.Context, identity.AgentID, int) ([]AgentControlCommand, error)
}
