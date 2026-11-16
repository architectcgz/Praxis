package session

import (
	"context"
	modelconfig "praxis/internal/core/model/config"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func (s *Service) Configuration(ctx context.Context, id identity.SessionID) (*sessionmodel.Configuration, error) {
	owner, err := s.core.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Configuration(), nil
}
func (s *Service) ModelOptions(ctx context.Context, id identity.SessionID) ([]modelconfig.Option, error) {
	config, err := s.Configuration(ctx, id)
	if err != nil {
		return nil, err
	}
	return config.Models.ListModels(), nil
}
