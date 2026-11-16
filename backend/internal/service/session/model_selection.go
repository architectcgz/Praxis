package session

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func (s *Service) SelectedModel(ctx context.Context, id identity.SessionID) (sessionmodel.ModelChoice, error) {
	owner, err := s.core.GetSession(ctx, id)
	if err != nil {
		return sessionmodel.ModelChoice{}, err
	}
	return owner.Choice(), nil
}
