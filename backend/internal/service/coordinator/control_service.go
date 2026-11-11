package coordinator

import (
	"context"
	coreevent "praxis/internal/core/event"
	"time"
)

// StopAgent 中断请求指定的 Run，保留尚未开始的输入。
func (s *Coordinator) StopAgent(ctx context.Context, request RunControl) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()

	active, applied, err := s.cancelRun(ctx, request, errRunInterrupted)
	if err != nil || !applied {
		return err
	}
	s.mu.Lock()
	source := coreevent.EventSource{
		SessionID: active.sessionID,
		BranchID:  active.branchID,
		BindingID: active.bindingID,
		AgentID:   request.AgentID,
		RunID:     active.runID,
	}
	s.mu.Unlock()
	s.loop.Emitter.Emit(coreevent.Event{
		Source:  source,
		Payload: coreevent.RunInterruptRequested{AppliedAt: time.Now().UTC()},
	})
	return nil
}
