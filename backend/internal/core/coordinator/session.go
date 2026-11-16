package coordinator

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type SessionView struct {
	QueuePaused bool
	Session     sessionmodel.SessionInfo
	Tasks       []taskmodel.Task
}

func (s *Coordinator) SelectSession(ctx context.Context, id identity.SessionID) (SessionView, error) {
	if err := s.enter(); err != nil {
		return SessionView{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return SessionView{}, err
	}
	defer unlock()
	if _, err := s.loadSession(ctx, id); err != nil {
		return SessionView{}, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return SessionView{}, err
	}
	view := SessionView{Session: info}
	s.mu.Lock()
	binding := s.schedules[id]
	s.mu.Unlock()
	if binding != nil {
		if binding.closing {
			return SessionView{}, errorcode.ErrRequestConflict
		}
		view.Tasks = binding.queue.List(0)
		view.QueuePaused = binding.queue.Paused()
	}
	return view, nil
}

func (s *Coordinator) bindSubmission(ctx context.Context, input taskmodel.Input) (*sessionSchedule, error) {
	owner, err := s.loadSession(ctx, input.SessionID)
	if err != nil {
		return nil, err
	}
	binding := &sessionSchedule{sessionID: input.SessionID, session: owner}
	s.mu.Lock()
	s.schedules[input.SessionID] = binding
	s.mu.Unlock()
	return binding, nil
}

func (s *Coordinator) releaseSessionState(id identity.SessionID) {
	s.mu.Lock()
	binding := s.schedules[id]
	delete(s.schedules, id)
	s.mu.Unlock()
	if binding != nil {
		binding.queue.Clear()
	}
}

// CloseSession 禁止新的任务，取消所属执行，并在完成信号后释放会话。
func (s *Coordinator) CloseSession(ctx context.Context, id identity.SessionID) error {
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
	if binding != nil {
		binding.closing = true
	}
	var active []*activeRun
	for _, run := range s.runs {
		if run.sessionID == id {
			active = append(active, run)
		}
	}
	s.mu.Unlock()
	if binding != nil {
		binding.queue.Clear()
	}
	unlock()
	for _, run := range active {
		run.cancel(context.Canceled)
	}
	for _, run := range active {
		select {
		case <-run.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	unlock, err = s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.releaseSession(ctx, id); err != nil {
		return err
	}
	s.releaseSessionState(id)
	return nil
}
