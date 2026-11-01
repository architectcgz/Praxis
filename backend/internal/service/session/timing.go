package session

import (
	"context"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/identity"
)

func (s *Service) ListAgentTimingRecords(ctx context.Context, agentID identity.AgentID, branchID identity.BranchID, limit int) ([]timing.Record, error) {
	owner, err := s.OpenedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return nil, err
	}
	return owner.ListAgentTimingRecords(agentID, branchID, limit), nil
}
