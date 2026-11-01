package session

import (
	"context"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// SelectModel 只更新已打开会话的模型选择，不修改已有执行。
func (s *Service) SelectModel(ctx context.Context, id identity.SessionID, choice sessionmodel.ModelChoice) error {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return err
	}
	owner.configMu.Lock()
	defer owner.configMu.Unlock()
	config, err := owner.config.Models.Get(choice.ProviderID, choice.ModelID, choice.ReasoningLevel)
	if err != nil {
		return fmt.Errorf("modelSelection: %v: %w", err, errorcode.ErrInvalidValue)
	}
	owner.modelSelection = sessionmodel.NewModelSelection(config)
	return nil
}

func (s *Service) SelectedModel(ctx context.Context, id identity.SessionID) (sessionmodel.ModelChoice, error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return sessionmodel.ModelChoice{}, err
	}
	owner.configMu.RLock()
	defer owner.configMu.RUnlock()
	return owner.modelSelection.Choice(), nil
}
