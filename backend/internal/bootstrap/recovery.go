package bootstrap

import (
	"context"
	"math"

	"praxis/internal/agent"
	"praxis/internal/identity"
	"praxis/internal/session"
	"praxis/internal/task"
	taskservice "praxis/internal/task/service"
)

// recoverySource 把启动恢复所需的持久化事实装配给 execution，不参与执行决策。
type recoverySource struct {
	tasks    taskservice.TaskStore
	sessions session.ListReader
	agents   agent.Store
}

func (s recoverySource) ListActive(ctx context.Context) ([]task.Task, error) {
	return s.tasks.ListActive(ctx)
}

func (s recoverySource) ListDispatchableAgents(ctx context.Context) ([]identity.AgentID, error) {
	sessions, err := s.sessions.List(ctx, math.MaxInt)
	if err != nil {
		return nil, err
	}
	var ids []identity.AgentID
	for _, value := range sessions {
		agents, err := s.agents.ListBySession(ctx, value.ID, math.MaxInt)
		if err != nil {
			return nil, err
		}
		for _, value := range agents {
			if value.State == agent.AgentIdle || value.State == agent.AgentWaiting {
				ids = append(ids, value.ID)
			}
		}
	}
	return ids, nil
}
