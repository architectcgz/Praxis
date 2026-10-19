package coordinator

import (
	"context"
	"math"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// RecoveryCatalog 把启动恢复所需的持久化事实装配给 execution，不参与执行决策。
type RecoveryCatalog struct {
	Executions repository.AgentExecution
	Sessions   repository.Session
	Agents     repository.Agent
}

func (s RecoveryCatalog) ListActive(ctx context.Context) ([]agentmodel.Execution, error) {
	return s.Executions.ListActive(ctx)
}

func (s RecoveryCatalog) ListDispatchableAgents(ctx context.Context) ([]identity.AgentID, error) {
	sessions, err := s.Sessions.List(ctx, math.MaxInt)
	if err != nil {
		return nil, err
	}
	var ids []identity.AgentID
	for _, value := range sessions {
		agents, err := s.Agents.ListBySession(ctx, value.ID, math.MaxInt)
		if err != nil {
			return nil, err
		}
		for _, value := range agents {
			if value.State == agentmodel.AgentIdle || value.State == agentmodel.AgentWaiting {
				ids = append(ids, value.ID)
			}
		}
	}
	return ids, nil
}
