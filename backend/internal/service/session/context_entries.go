package session

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

func (s *Service) ListAgentEntries(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID, head, before string, limit int) (sessionmodel.HistoryPage, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	return owner.AgentPage(ctx, branchID, agentID, head, before, limit)
}

func (s *Service) Entry(ctx context.Context, sessionID identity.SessionID, id string) (*sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.Entry(ctx, id)
}
