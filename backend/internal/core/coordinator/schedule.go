package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/entry"
	runmodel "praxis/internal/core/run"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (r *Coordinator) schedule(ctx context.Context, sessionID identity.SessionID) error {
	input, schedule, active, err := r.claimTask(ctx, sessionID)
	if err != nil || active == nil {
		return err
	}
	return r.startNext(ctx, input, schedule, active)
}

func (r *Coordinator) claimTask(ctx context.Context, sessionID identity.SessionID) (taskmodel.Task, *sessionSchedule, *activeRun, error) {
	unlock, err := r.lockSession(ctx, sessionID)
	if err != nil {
		return taskmodel.Task{}, nil, nil, err
	}
	defer unlock()
	return r.claimTaskLocked(ctx, sessionID)
}

// claimTaskLocked 的调用方持有会话命令锁；队列已由会话命令锁串行化，此处单次临界区原子完成检查、领取与占位。
func (r *Coordinator) claimTaskLocked(ctx context.Context, sessionID identity.SessionID) (taskmodel.Task, *sessionSchedule, *activeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	schedule := r.schedules[sessionID]
	if r.closed || schedule == nil || schedule.closing {
		return taskmodel.Task{}, nil, nil, errCoordinatorClosed
	}
	if schedule.active != nil {
		return taskmodel.Task{}, nil, nil, nil
	}
	input, ok := schedule.queue.Next()
	if !ok {
		return taskmodel.Task{}, nil, nil, nil
	}
	active := newActiveRun(ctx, schedule.session)
	schedule.active = active
	return input, schedule, active, nil
}

func (r *Coordinator) startNext(ctx context.Context, input taskmodel.Task, schedule *sessionSchedule, active *activeRun) error {
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
	unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
	schedule.queue.Complete(input)
	r.mu.Lock()
	active.config = config
	if schedule.closing {
		err = errorcode.ErrRequestConflict
	} else {
		err = r.launchLocked(run, active)
	}
	r.mu.Unlock()
	if err == nil {
		notification := taskQueueEvent(schedule)
		unlock()
		r.emitter.Emit(notification)
		return nil
	}
	unlock()
	endCtx, cancelEnd := r.transitionContext()
	defer cancelEnd()
	if saveErr := run.Settle(endCtx, entry.OutcomeInterrupted, entry.RunFailureCancelled, ""); saveErr != nil {
		err = errorcode.ErrStorage.Wrap(errors.Join(err, saveErr))
	}
	return r.finishTaskStart(input, schedule, active, false, err)
}

func (r *Coordinator) finishTaskStart(input taskmodel.Task, schedule *sessionSchedule, active *activeRun, retry bool, err error) error {
	unlock, _ := r.lockSession(context.Background(), input.SessionID)
	storageFailed := errors.Is(err, errorcode.ErrStorage)
	if storageFailed {
		schedule.queue.Pause()
	}
	if retry {
		schedule.queue.Retry(input)
	} else {
		schedule.queue.Complete(input)
	}
	r.mu.Lock()
	schedule.active = nil
	close(active.done)
	r.mu.Unlock()
	active.cancel(nil)
	notification := taskQueueEvent(schedule)
	unlock()
	r.emitter.Emit(notification)
	if storageFailed {
		r.emitQueueStorageFailure(input.SessionID, err)
	}
	return err
}

func (r *Coordinator) newTaskRun(input taskmodel.Task, schedule *sessionSchedule) (*runmodel.Run, *sessionmodel.Configuration, error) {
	config, model, err := schedule.session.RunConfiguration()
	if err != nil {
		return nil, nil, err
	}
	definition, err := config.Definition(agentmodel.DefinitionPrimary)
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
