package coordinator

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// SelectModel 定位已打开的会话更新选择，再尝试唤醒该会话的预约，不影响其他会话。
func (s *Coordinator) SelectModel(ctx context.Context, sessionID identity.SessionID, choice sessionmodel.ModelChoice) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := s.sessions.SelectModel(ctx, sessionID, choice); err != nil {
		unlock()
		return err
	}
	s.mu.Lock()
	bindingID := ""
	if binding := s.bindings[sessionID]; binding != nil && !binding.closing {
		bindingID = binding.id
	}
	s.mu.Unlock()
	unlock()
	if bindingID != "" {
		if err := s.scheduleBinding(ctx, sessionID, bindingID, true); err != nil {
			s.logger.Errorf("activate queued input: %v", err)
		}
	}
	return nil
}
