package session

import (
	"context"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/identity"
)

func (s *Service) ListAgentTimings(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID, limit int) ([]timing.View, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListAgentTimings(agentID, branchID, limit), nil
}
