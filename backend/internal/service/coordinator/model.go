package coordinator

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

// SelectModel 委托会话服务更新选择，再尝试唤醒该会话的预约，不影响其他会话。
func (s *Coordinator) SelectModel(ctx context.Context, sessionID identity.SessionID, choice sessionmodel.ModelChoice) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	if err := s.sessions.SelectModel(ctx, sessionID, choice); err != nil {
		return err
	}
	if choice.ModelID == "" {
		return nil
	}
	s.mu.Lock()
	var ids []string
	for id, binding := range s.bindings {
		if binding.sessionID == sessionID && !binding.closing {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		if err := s.scheduleBinding(ctx, id, true); err != nil {
			s.logger.Errorf("activate queued input: %v", err)
		}
	}
	return nil
}
