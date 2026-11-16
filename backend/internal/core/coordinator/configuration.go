package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/security"
	"praxis/internal/identity"
)

// ReloadSessionConfig 只更新指定会话；主执行和子执行均空闲时才允许重新加载。
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
	binding := s.schedules[id]
	if binding != nil {
		binding.reloading = true
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if binding != nil {
			binding.reloading = false
		}
		s.mu.Unlock()
	}()
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if binding != nil {
		binding.queue.Clear()
		s.loop.Emitter.Emit(taskQueueEvent(binding))
	}
	config, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return err
	}
	config.ApprovalMode = security.EffectiveApprovalMode(config.GlobalApprovalMode, info.ApprovalMode)
	owner.Configure(config, owner.Choice())
	return nil
}
