package compose

import (
	"context"
	"math"

	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/identity"
	"praxis/internal/repository"
)

// recoverySource 只读取启动恢复所需的持久化事实。
type recoverySource struct {
	repository.TaskRepository
	sessions repository.SessionListRepository
	agents   repository.SessionAgentRepository
}

func (s recoverySource) ListDispatchableAgents(ctx context.Context) ([]identity.AgentID, error) {
	sessions, err := s.sessions.List(ctx, math.MaxInt)
	if err != nil {
		return nil, err
	}
	var ids []identity.AgentID
	for _, session := range sessions {
		agents, err := s.agents.ListBySession(ctx, session.ID, math.MaxInt)
		if err != nil {
			return nil, err
		}
		for _, agent := range agents {
			if agent.State == agentmodel.AgentIdle || agent.State == agentmodel.AgentWaiting {
				ids = append(ids, agent.ID)
			}
		}
	}
	return ids, nil
}
