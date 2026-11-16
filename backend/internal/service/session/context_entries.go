package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func (s *Service) ListAgentEntries(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, head, before string, limit int) (sessionmodel.HistoryPage, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	return owner.AgentPage(ctx, agentID, head, before, limit)
}

func (s *Service) Entry(ctx context.Context, sessionID identity.SessionID, id string) (*sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.Entry(ctx, id)
}
