package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/security"
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

type SessionContent struct {
	Agents []sessionmodel.AgentView
	Page   sessionmodel.HistoryPage
	Usage  sessionmodel.UsageSnapshot
}

func (s *Coordinator) GetSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	value := s.registry.get(id)
	s.mu.Unlock()
	if value == nil {
		return nil, errorcode.ErrNotFound
	}
	return value, nil
}

func (s *Coordinator) LoadSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.loadSession(ctx, id)
}

func (s *Coordinator) loadSession(ctx context.Context, id identity.SessionID) (*sessionmodel.Session, error) {
	s.mu.Lock()
	value := s.registry.get(id)
	s.mu.Unlock()
	if value != nil {
		return value, nil
	}
	owner, err := s.sessionRecords.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if info.State != sessionmodel.SessionActive {
		return nil, errorcode.ErrRequestConflict
	}
	config, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	config.ApprovalMode = security.EffectiveApprovalMode(config.GlobalApprovalMode, info.ApprovalMode)
	owner.Configure(config, info.ModelChoice)
	s.mu.Lock()
	s.registry.add(id, owner)
	if s.schedules[id] == nil {
		s.schedules[id] = &sessionSchedule{
			sessionID: id,
			session:   owner,
		}
	}
	s.mu.Unlock()
	return owner, nil
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
	if _, err := s.loadSession(ctx, input.SessionID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	binding := s.schedules[input.SessionID]
	s.mu.Unlock()
	return binding, nil
}

func (s *Coordinator) releaseSession(ctx context.Context, id identity.SessionID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.registry.remove(id)
	binding := s.schedules[id]
	delete(s.schedules, id)
	s.mu.Unlock()
	if binding != nil {
		binding.queue.Clear()
	}
	return nil
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
	var active *activeRun
	if binding != nil {
		active = binding.active
	}
	s.mu.Unlock()
	if binding != nil {
		binding.queue.Clear()
	}
	unlock()
	if active != nil {
		active.cancel(context.Canceled)
		select {
		case <-active.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	unlock, err = s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	return s.releaseSession(ctx, id)
}

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
	binding := s.schedules[id]
	if binding != nil && binding.active != nil {
		s.mu.Unlock()
		return agentmodel.ErrExecuting
	}
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
	if err := s.schedule(ctx, sessionID); err != nil {
		s.logger.Errorf("activate queued task: %v", err)
	}
	return nil
}

func (s *Coordinator) AgentHistory(ctx context.Context, id identity.SessionID, agentID identity.AgentID, before *int64, limit int) (sessionmodel.HistoryPage, error) {
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	return owner.AgentPage(ctx, agentID, before, limit)
}

func (s *Coordinator) SessionContent(ctx context.Context, id identity.SessionID) (SessionContent, error) {
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return SessionContent{}, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return SessionContent{}, err
	}
	definition, err := owner.Definition(agentmodel.DefinitionPrimary)
	if err != nil {
		return SessionContent{}, err
	}
	agents, err := owner.Agents(ctx, info, definition)
	if err != nil {
		return SessionContent{}, err
	}
	usage, err := owner.UsageSnapshot(ctx)
	if err != nil {
		return SessionContent{}, err
	}
	head := usage.HistoryEnds[info.MainAgentID()]
	page, err := owner.AgentPage(ctx, info.MainAgentID(), &head, 50)
	return SessionContent{
		Agents: agents,
		Page:   page,
		Usage:  usage,
	}, err
}
