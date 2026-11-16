package coordinator

import (
	"context"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Coordinator) PauseQueue(ctx context.Context, id identity.SessionID) error {
	return s.changeQueue(ctx, id, true)
}
func (s *Coordinator) ResumeQueue(ctx context.Context, id identity.SessionID) error {
	return s.changeQueue(ctx, id, false)
}
func (s *Coordinator) changeQueue(ctx context.Context, id identity.SessionID, pause bool) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	binding := s.schedules[id]
	s.mu.Unlock()
	if binding == nil {
		unlock()
		return errorcode.ErrNotFound
	}
	if pause {
		binding.queue.Pause()
	} else {
		binding.queue.Resume()
	}
	notification := taskQueueEvent(binding)
	unlock()
	s.loop.Emitter.Emit(notification)
	if !pause {
		s.mu.Lock()
		active := s.runs[runKey{identity.AgentID(id)}]
		s.mu.Unlock()
		// 显式恢复可以等待已取消的旧执行收尾，不能让恢复意图丢在占用窗口里。
		if active != nil && active.ctx.Err() != nil {
			select {
			case <-active.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return s.schedule(ctx, id, true)
	}
	return nil
}
