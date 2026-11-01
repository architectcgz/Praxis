package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID) (page sessionentry.List, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return page, err
	}
	entries, err := owner.BranchEntries(ctx, branchID)
	if err != nil {
		return page, err
	}
	page.Entries = sessionentry.Values(entries)
	return page, nil
}
