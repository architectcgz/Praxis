package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	entryPayload "praxis/internal/core/entry/payload"
	"praxis/internal/core/loop"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
)

func (r *Coordinator) schedule(ctx context.Context, sessionID identity.SessionID) error {
	input, binding, active, err := r.claimTask(ctx, sessionID)
	if err != nil || active == nil {
		return err
	}
	return r.startNext(ctx, input, binding, active)
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
	binding := r.schedules[sessionID]
	if r.closed || binding == nil || binding.closing || binding.reloading {
		r.mu.Unlock()
		return taskmodel.Task{}, nil, nil, errCoordinatorClosed
	}
	active := binding.active
	r.mu.Unlock()
	if active != nil {
		return taskmodel.Task{}, nil, nil, nil
	}
	input, ok := binding.queue.Next()
	if !ok {
		return taskmodel.Task{}, nil, nil, nil
	}
	active = newActiveRun(ctx, binding.session)
	r.mu.Lock()
	binding.active = active
	r.mu.Unlock()
	return input, binding, active, nil
}

func (r *Coordinator) startNext(ctx context.Context, input taskmodel.Task, binding *sessionSchedule, active *activeRun) error {
	prepareCtx, cancel := context.WithCancelCause(active.ctx)
	stop := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
	defer stop()
	defer cancel(nil)

	run, config, err := r.newTaskRun(input, binding)
	if err != nil {
		return r.finishTaskStart(input, binding, active, true, err)
	}
	r.mu.Lock()
	active.run = run
	r.mu.Unlock()
	prepared, err := run.Prepare(prepareCtx, input.Prompt)
	if err != nil {
		canceled := errors.Is(err, context.Canceled) && errors.Is(context.Cause(prepareCtx), agentrun.ErrInterrupted)
		if prepared {
			if saveErr := run.SettlePreparation(prepareCtx, err); saveErr != nil {
				return r.finishTaskStart(input, binding, active, false, loop.NewFailure(loop.FailureStorage, errors.Join(err, saveErr)))
			}
		}
		if canceled {
			return r.finishTaskStart(input, binding, active, false, nil)
		}
		return r.finishTaskStart(input, binding, active, !prepared, err)
	}
	unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
	binding.queue.Complete(input)
	r.mu.Lock()
	active.config = config
	err = r.launchLocked(run, active)
	r.mu.Unlock()
	if err == nil {
		notification := taskQueueEvent(binding)
		unlock()
		r.loop.Emitter.Emit(notification)
		return nil
	}
	unlock()
	endCtx, cancelEnd := r.transitionContext()
	defer cancelEnd()
	if saveErr := run.Settle(endCtx, entryPayload.OutcomeInterrupted, entryPayload.RunFailureCancelled, ""); saveErr != nil {
		err = loop.NewFailure(loop.FailureStorage, errors.Join(err, saveErr))
	}
	return r.finishTaskStart(input, binding, active, false, err)
}

func (r *Coordinator) finishTaskStart(input taskmodel.Task, binding *sessionSchedule, active *activeRun, retry bool, err error) error {
	unlock, _ := r.lockSession(context.Background(), input.SessionID)
	storageFailed := loop.FailureClassification(err) == loop.FailureStorage
	if storageFailed {
		binding.queue.Pause()
	}
	if retry {
		binding.queue.Retry(input)
	} else {
		binding.queue.Complete(input)
	}
	r.mu.Lock()
	binding.active = nil
	close(active.done)
	r.mu.Unlock()
	active.cancel(nil)
	notification := taskQueueEvent(binding)
	unlock()
	r.loop.Emitter.Emit(notification)
	if storageFailed {
		r.emitQueueStorageFailure(input.SessionID, err)
	}
	return err
}

func (r *Coordinator) newTaskRun(input taskmodel.Task, binding *sessionSchedule) (*agentrun.Run, *sessionmodel.Configuration, error) {
	config, model, err := binding.session.RunConfiguration()
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
	}, binding.session.BindHistory(agentID))
	return run, config, nil
}
