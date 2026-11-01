package session

import (
	"context"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// SelectModel 只在当前会话已加载的目录中切换选择，不修改已有执行。
func (s *Service) SelectModel(ctx context.Context, sessionID identity.SessionID, choice sessionmodel.ModelChoice) error {
	if err := RequireWritable(ctx, s.sessions, sessionID); err != nil {
		return err
	}
	owner, err := s.openSession(ctx, sessionID)
	if err != nil {
		return err
	}
	return owner.use(ctx, func() error {
		if choice == (sessionmodel.ModelChoice{}) {
			owner.modelSelection = sessionmodel.ModelSelection{}
			return nil
		}
		config, err := owner.config.Models.Get(choice.ProviderID, choice.ModelID, choice.ReasoningLevel)
		if err != nil {
			return fmt.Errorf("modelSelection: %v: %w", err, errorcode.ErrInvalidValue)
		}
		owner.modelSelection = sessionmodel.NewModelSelection(config)
		return nil
	})
}

func (s *Service) SelectedModel(ctx context.Context, sessionID identity.SessionID) (sessionmodel.ModelChoice, error) {
	owner, err := s.openSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.ModelChoice{}, err
	}
	var choice sessionmodel.ModelChoice
	err = owner.use(ctx, func() error { choice = owner.modelSelection.Choice(); return nil })
	return choice, err
}
