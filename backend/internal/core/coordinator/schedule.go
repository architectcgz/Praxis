package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/entry"
	"praxis/internal/core/loop"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
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

// claimTaskLocked 的调用方持有会话命令锁；调度器锁只用于定位和登记，不嵌套队列锁。
func (r *Coordinator) claimTaskLocked(ctx context.Context, sessionID identity.SessionID) (taskmodel.Task, *sessionSchedule, *activeRun, error) {
	r.mu.Lock()
	schedule := r.schedules[sessionID]
	if r.closed || schedule == nil || schedule.closing || schedule.reloading {
		r.mu.Unlock()
		return taskmodel.Task{}, nil, nil, errCoordinatorClosed
	}
	active := schedule.active
	r.mu.Unlock()
	if active != nil {
		return taskmodel.Task{}, nil, nil, nil
	}
	input, ok := schedule.queue.Next()
	if !ok {
		return taskmodel.Task{}, nil, nil, nil
	}
	active = newActiveRun(ctx, schedule.session)
	r.mu.Lock()
	schedule.active = active
	r.mu.Unlock()
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
		canceled := errors.Is(err, context.Canceled) && errors.Is(context.Cause(prepareCtx), agentrun.ErrInterrupted)
		if prepared {
			if saveErr := run.SettlePreparation(prepareCtx, err); saveErr != nil {
				return r.finishTaskStart(input, schedule, active, false, loop.NewFailure(loop.FailureStorage, errors.Join(err, saveErr)))
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
	err = r.launchLocked(run, active)
	r.mu.Unlock()
	if err == nil {
		notification := taskQueueEvent(schedule)
		unlock()
		r.loop.Emitter.Emit(notification)
		return nil
	}
	unlock()
	endCtx, cancelEnd := r.transitionContext()
	defer cancelEnd()
	if saveErr := run.Settle(endCtx, entry.OutcomeInterrupted, entry.RunFailureCancelled, ""); saveErr != nil {
		err = loop.NewFailure(loop.FailureStorage, errors.Join(err, saveErr))
	}
	return r.finishTaskStart(input, schedule, active, false, err)
}

func (r *Coordinator) finishTaskStart(input taskmodel.Task, schedule *sessionSchedule, active *activeRun, retry bool, err error) error {
	unlock, _ := r.lockSession(context.Background(), input.SessionID)
	storageFailed := loop.FailureClassification(err) == loop.FailureStorage
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
	r.loop.Emitter.Emit(notification)
	if storageFailed {
		r.emitQueueStorageFailure(input.SessionID, err)
	}
	return err
}

func (r *Coordinator) newTaskRun(input taskmodel.Task, schedule *sessionSchedule) (*agentrun.Run, *sessionmodel.Configuration, error) {
	config, model, err := schedule.session.RunConfiguration()
	if err != nil {
		return nil, nil, err
	}
	definition, err := config.Definition(agentmodel.DefinitionPrimary)
	if err != nil {
		return nil, nil, err
	}
	agentID := identity.AgentID(input.SessionID)
	run := agentrun.New(r.runDependencies, agentrun.Config{
		SourceTaskID: input.ID,
		Definition:   definition,
		Model:        model,
	}, schedule.session.BindHistory(agentID))
	return run, config, nil
}
