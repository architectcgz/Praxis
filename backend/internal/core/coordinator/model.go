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
	owner, err := s.GetSession(ctx, sessionID)
	if err != nil {
		unlock()
		return err
	}
	if _, err := owner.Configuration().Models.Get(choice.ProviderID, choice.ModelID, choice.ReasoningLevel); err != nil {
		unlock()
		return err
	}
	if err := s.sessionRecords.SetModelChoice(ctx, sessionID, choice, s.runDependencies.Clock.Now()); err != nil {
		unlock()
		return err
	}
	owner.Configure(owner.Configuration(), choice)
	unlock()
	if err := s.schedule(ctx, sessionID, true); err != nil {
		s.logger.Errorf("activate queued task: %v", err)
	}

	return nil
}
