package coordinator

import (
	"context"
	"errors"
	"praxis/internal/agent"
	"praxis/internal/engine/approval"
	runmodel "praxis/internal/engine/run"
	sessionmodel "praxis/internal/engine/session"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/security"
	"praxis/internal/task"
	"time"
)

// ApprovalSettings 包含全局默认值、会话覆盖和当前有效审批模式。
type ApprovalSettings struct {
	SessionID     identity.SessionID
	GlobalMode    security.ApprovalMode
	SessionMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

// SessionView 是前端打开会话所需的完整初始快照，后续变化通过事件推送。
type SessionView struct {
	QueuePaused      bool
	Session          sessionmodel.SessionInfo
	Tasks            []task.Task
	Models           []model.Option
	ModelChoice      model.Choice
	Approval         ApprovalSettings
	PendingApprovals []approval.Approval
}

// loadSchedule 的调用方必须持有会话命令锁。
func (s *Coordinator) loadSchedule(ctx context.Context, id identity.SessionID) (*sessionSchedule, error) {
	s.mu.Lock()
	existing := s.schedules[id]
	closed := s.closed
	closing := existing != nil && existing.closing
	s.mu.Unlock()
	if closed {
		return nil, errCoordinatorClosed
	}
	if closing {
		return nil, errorcode.ErrRequestConflict
	}
	if existing != nil {
		return existing, nil
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
	owner, err := s.sessionLoader.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	owner.Configure(config, info.ModelChoice, info.ApprovalMode)
	schedule := &sessionSchedule{
		sessionID: id,
		session:   owner,
		subagents: runmodel.NewSubagents(),
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
		defer cancel()
		_ = owner.Close(closeCtx)
		return nil, errCoordinatorClosed
	}
	s.schedules[id] = schedule
	s.mu.Unlock()
	return schedule, nil
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
	schedule, err := s.loadSchedule(ctx, id)
	if err != nil {
		return SessionView{}, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return SessionView{}, err
	}
	pendingApprovals, err := s.approvals.ListSessionApprovals(ctx, id)
	if err != nil {
		return SessionView{}, err
	}
	config := schedule.session.Configuration()
	s.mu.Lock()
	paused, tasks := schedule.queue.Paused(), schedule.queue.List(0)
	s.mu.Unlock()
	return SessionView{
		QueuePaused: paused,
		Session:     info,
		Tasks:       tasks,
		Models:      config.Models.ListModels(),
		ModelChoice: schedule.session.Choice(),
		Approval: ApprovalSettings{
			SessionID:     id,
			GlobalMode:    config.GlobalApprovalMode,
			SessionMode:   info.ApprovalMode,
			EffectiveMode: schedule.session.ApprovalMode(),
		},
		PendingApprovals: pendingApprovals,
	}, nil
}

func (s *Coordinator) releaseSession(ctx context.Context, id identity.SessionID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	schedule := s.schedules[id]
	if schedule != nil {
		schedule.queue.Clear()
	}
	s.mu.Unlock()
	if schedule == nil {
		return nil
	}
	err := schedule.session.Close(ctx)
	if ctx.Err() == nil {
		s.mu.Lock()
		delete(s.schedules, id)
		s.mu.Unlock()
	}
	return errors.Join(err, ctx.Err())
}

// CloseSession 禁止新的任务，取消所属执行，并在完成信号后释放会话。
func (s *Coordinator) CloseSession(ctx context.Context, id identity.SessionID) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	ctx, cancel := context.WithTimeout(ctx, s.shutdownTimeout)
	defer cancel()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	schedule := s.schedules[id]
	var active *activeRun
	if schedule != nil {
		schedule.closing = true
		active = schedule.active
		schedule.queue.Clear()
	}
	s.mu.Unlock()
	unlock()
	if schedule != nil {
		schedule.subagents.CancelAll(context.Canceled)
	}
	if active != nil {
		active.cancel(context.Canceled)
		select {
		case <-active.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if schedule != nil {
		if err := schedule.subagents.Wait(ctx); err != nil {
			return err
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
	queueEvent, reloadEvent, err := s.reloadIdleSession(ctx, id)
	if err != nil {
		return err
	}
	s.logger.Infof("session configuration reloaded session=%s", id)
	s.emitter.Emit(queueEvent)
	s.emitter.Emit(reloadEvent)
	return nil
}

func (s *Coordinator) reloadIdleSession(ctx context.Context, id identity.SessionID) (event.Event, event.Event, error) {
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return event.Event{}, event.Event{}, err
	}
	defer unlock()
	s.mu.Lock()
	schedule := s.schedules[id]
	closing := schedule != nil && schedule.closing
	busy := schedule != nil && (schedule.active != nil || schedule.subagents.Active())
	s.mu.Unlock()
	if schedule == nil {
		return event.Event{}, event.Event{}, errorcode.ErrNotFound
	}
	if closing {
		return event.Event{}, event.Event{}, errorcode.ErrRequestConflict
	}
	if busy {
		return event.Event{}, event.Event{}, agent.ErrExecuting
	}
	config, err := s.loadConfig(ctx)
	if err != nil {
		return event.Event{}, event.Event{}, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return event.Event{}, event.Event{}, err
	}
	choice := info.ModelChoice
	if choice.ModelID != "" {
		if _, err := config.Models.Get(choice); err != nil {
			return event.Event{}, event.Event{}, err
		}
	}
	schedule.session.Configure(config, choice, info.ApprovalMode)
	s.mu.Lock()
	schedule.queue.Clear()
	queueEvent := taskQueueEvent(schedule)
	s.mu.Unlock()
	reloadEvent := event.Event{
		Source: event.EventSource{
			SessionID: id,
			AgentID:   identity.AgentID(id),
		},
		Payload: event.SessionConfigReloaded{
			Models:        config.Models.ListModels(),
			ModelChoice:   schedule.session.Choice(),
			GlobalMode:    config.GlobalApprovalMode,
			SessionMode:   info.ApprovalMode,
			EffectiveMode: schedule.session.ApprovalMode(),
		},
	}
	return queueEvent, reloadEvent, nil
}

// SetApprovalMode 在持久化成功后更新会话内存中的审批覆盖，不影响队列和配置快照。
func (s *Coordinator) SetApprovalMode(ctx context.Context, id identity.SessionID, mode *security.ApprovalMode) (ApprovalSettings, error) {
	if err := s.enter(); err != nil {
		return ApprovalSettings{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return ApprovalSettings{}, err
	}
	defer unlock()
	s.mu.Lock()
	schedule := s.schedules[id]
	closing := schedule != nil && schedule.closing
	s.mu.Unlock()
	if schedule == nil {
		return ApprovalSettings{}, errorcode.ErrNotFound
	}
	if closing {
		return ApprovalSettings{}, errorcode.ErrRequestConflict
	}
	info, err := s.sessionRecords.SetApprovalMode(ctx, id, mode, time.Now().UTC())
	if err != nil {
		return ApprovalSettings{}, err
	}
	schedule.session.SetApprovalMode(info.ApprovalMode)
	config := schedule.session.Configuration()
	return ApprovalSettings{
		SessionID:     id,
		GlobalMode:    config.GlobalApprovalMode,
		SessionMode:   info.ApprovalMode,
		EffectiveMode: schedule.session.ApprovalMode(),
	}, nil
}

// SelectModel 定位已打开的会话更新选择，再尝试唤醒该会话的预约，不影响其他会话。
func (s *Coordinator) SelectModel(ctx context.Context, sessionID identity.SessionID, choice model.Choice) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	schedule := s.schedules[sessionID]
	closing := schedule != nil && schedule.closing
	s.mu.Unlock()
	if schedule == nil {
		unlock()
		return errorcode.ErrNotFound
	}
	if closing {
		unlock()
		return errorcode.ErrRequestConflict
	}
	owner := schedule.session
	if _, err := owner.Configuration().Models.Get(choice); err != nil {
		unlock()
		return err
	}
	if err := s.sessionRecords.SetModelChoice(ctx, sessionID, choice, time.Now().UTC()); err != nil {
		unlock()
		return err
	}
	owner.SetChoice(choice)
	unlock()
	if err := s.schedule(ctx, sessionID); err != nil {
		s.logger.Errorf("activate queued task: %v", err)
	}
	return nil
}

func (s *Coordinator) AgentHistory(ctx context.Context, id identity.SessionID, agentID identity.AgentID, before *int64, limit int) (sessionmodel.HistoryPage, error) {
	if err := s.enter(); err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	defer s.running.Done()
	s.mu.Lock()
	schedule := s.schedules[id]
	closing := schedule != nil && schedule.closing
	s.mu.Unlock()
	if schedule == nil {
		return sessionmodel.HistoryPage{}, errorcode.ErrNotFound
	}
	if closing {
		return sessionmodel.HistoryPage{}, errorcode.ErrRequestConflict
	}
	return schedule.session.AgentPage(ctx, agentID, before, limit)
}
