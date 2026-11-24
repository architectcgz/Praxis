package coordinator

import (
	"context"
	"errors"
	"praxis/internal/agent"
	runmodel "praxis/internal/engine/run"
	sessionmodel "praxis/internal/engine/session"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/task"
)

func (r *Coordinator) schedule(ctx context.Context, sessionID identity.SessionID) error {
	input, schedule, active, err := r.claimTask(ctx, sessionID)
	if err != nil || active == nil {
		return err
	}
	return r.startNext(ctx, input, schedule, active)
}

func (r *Coordinator) claimTask(ctx context.Context, sessionID identity.SessionID) (task.Task, *sessionSchedule, *activeRun, error) {
	unlock, err := r.lockSession(ctx, sessionID)
	if err != nil {
		return task.Task{}, nil, nil, err
	}
	defer unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claimTaskLocked(ctx, sessionID)
}

// claimTaskLocked 在会话命令锁和 mu 下原子完成检查、领取与占位。
func (r *Coordinator) claimTaskLocked(ctx context.Context, sessionID identity.SessionID) (task.Task, *sessionSchedule, *activeRun, error) {
	schedule := r.schedules[sessionID]
	if r.closed || schedule == nil || schedule.closing {
		return task.Task{}, nil, nil, errCoordinatorClosed
	}
	if schedule.active != nil {
		return task.Task{}, nil, nil, nil
	}
	input, ok := schedule.queue.Next()
	if !ok {
		return task.Task{}, nil, nil, nil
	}
	active := newActiveRun(ctx, schedule.session)
	schedule.active = active
	return input, schedule, active, nil
}

func (r *Coordinator) startNext(ctx context.Context, input task.Task, schedule *sessionSchedule, active *activeRun) error {
	prepareCtx, cancel := context.WithCancelCause(active.ctx)
	stop := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
	defer stop()
	defer cancel(nil)

	run, config, err := r.newTaskRun(input, schedule)
	if err != nil {
		return r.finishTaskStart(input, schedule, active, true, err)
	}
	r.mu.Lock()
	active.run = run
	r.mu.Unlock()
	prepared, err := run.Prepare(prepareCtx, input.Prompt)
	if err != nil {
		canceled := errors.Is(err, context.Canceled) && errors.Is(context.Cause(prepareCtx), runmodel.ErrInterrupted)
		if prepared {
			if saveErr := run.SettlePreparation(prepareCtx, err); saveErr != nil {
				return r.finishTaskStart(input, schedule, active, false, errorcode.ErrStorage.Wrap(errors.Join(err, saveErr)))
			}
		}
		if canceled {
			return r.finishTaskStart(input, schedule, active, false, nil)
		}
		return r.finishTaskStart(input, schedule, active, !prepared, err)
	}
	unlock, err := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
	if err == nil {
		r.mu.Lock()
		schedule.queue.Complete(input)
		active.config = config
		switch {
		case active.ctx.Err() != nil:
			err = context.Cause(active.ctx)
		case schedule.closing:
			err = errorcode.ErrRequestConflict
		default:
			err = r.launchLocked(run, active)
		}
		var notification event.Event
		if err == nil {
			notification = taskQueueEvent(schedule)
		}
		r.mu.Unlock()
		unlock()
		if err == nil {
			r.emitter.Emit(notification)
			return nil
		}
	}
	var saveErr error
	if errors.Is(err, errCoordinatorClosed) || errors.Is(err, errorcode.ErrRequestConflict) {
		// 关闭标记可能先于取消信号到达；此时未启动的 Run 仍按关闭取消结算。
		saveErr = run.Settle(active.ctx, entry.OutcomeInterrupted, entry.RunFailureCancelled, "")
	} else {
		saveErr = run.SettlePreparation(active.ctx, err)
	}
	if saveErr != nil {
		err = errorcode.ErrStorage.Wrap(errors.Join(err, saveErr))
	}
	return r.finishTaskStart(input, schedule, active, false, err)
}

func (r *Coordinator) finishTaskStart(input task.Task, schedule *sessionSchedule, active *activeRun, retry bool, err error) error {
	storageFailed := errors.Is(err, errorcode.ErrStorage)
	// 必须释放占位；该内存状态不依赖可能已超时的会话命令锁。
	r.mu.Lock()
	if storageFailed {
		schedule.queue.Pause()
	}
	if retry {
		schedule.queue.Retry(input)
	} else {
		schedule.queue.Complete(input)
	}
	schedule.active = nil
	notification := taskQueueEvent(schedule)
	r.mu.Unlock()
	active.cancel(nil)
	close(active.done)
	r.emitter.Emit(notification)
	if storageFailed {
		r.emitQueueStorageFailure(input.SessionID, err)
	}
	return err
}

func (r *Coordinator) newTaskRun(input task.Task, schedule *sessionSchedule) (*runmodel.Run, *sessionmodel.Configuration, error) {
	config, model, err := schedule.session.RunConfiguration()
	if err != nil {
		return nil, nil, err
	}
	definition, err := config.Definition(agent.DefinitionPrimary)
	if err != nil {
		return nil, nil, err
	}
	agentID := identity.AgentID(input.SessionID)
	run := runmodel.New(r.runDependencies, runmodel.Config{
		SourceTaskID: input.ID,
		Definition:   definition,
		Model:        model,
	}, schedule.session.BindHistory(agentID))
	return run, config, nil
}
