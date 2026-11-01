package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID) ([]sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	entries, err := owner.BranchEntries(ctx, branchID)
	if err != nil {
		return nil, err
	}
	return sessionentry.Values(entries), nil
}
