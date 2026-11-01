package session

import (
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
)

// SelectModel 只更新已打开会话的模型选择，不修改已有执行。
func (s *Session) SelectModel(choice sessionmodel.ModelChoice) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	config, err := s.config.Models.Get(choice.ProviderID, choice.ModelID, choice.ReasoningLevel)
	if err != nil {
		return fmt.Errorf("modelSelection: %v: %w", err, errorcode.ErrInvalidValue)
	}
	s.modelSelection = sessionmodel.NewModelSelection(config)
	return nil
}

func (s *Session) SelectedModel() sessionmodel.ModelChoice {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.modelSelection.Choice()
}
