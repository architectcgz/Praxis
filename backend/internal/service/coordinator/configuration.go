package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	"praxis/internal/identity"
)

// ReloadSessionConfig 只更新指定会话；所有分支和子执行均空闲时才允许重新加载。
func (s *Coordinator) ReloadSessionConfig(ctx context.Context, id identity.SessionID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	if err := s.reloadIdleSession(ctx, id); err != nil {
		return err
	}
	s.logger.Infof("session configuration reloaded session=%s", id)
	s.loop.Emitter.Emit(coreevent.Event{
		Source: coreevent.EventSource{
			SessionID: id,
			AgentID:   identity.AgentID(id),
		},
		Payload: coreevent.SessionConfigReloaded{},
	})
	return nil
}

func (s *Coordinator) reloadIdleSession(ctx context.Context, id identity.SessionID) error {
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	s.mu.Lock()
	for _, active := range s.runs {
		if active.sessionID == id {
			s.mu.Unlock()
			return agentmodel.ErrExecuting
		}
	}
	s.mu.Unlock()
	return s.sessions.ReloadConfiguration(ctx, id)
}
