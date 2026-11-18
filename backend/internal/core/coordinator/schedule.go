package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/entry"
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
	active = newActiveRun(ctx, sessionID)
	r.mu.Lock()
	binding.active = active
	r.mu.Unlock()
	return input, binding, active, nil
}

func (r *Coordinator) startNext(ctx context.Context, input taskmodel.Task, binding *sessionSchedule, active *activeRun) (err error) {
	prepareCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(active.ctx, cancel)
	defer func() { stop(); cancel() }()
	defer func() {
		if err == nil {
			return
		}
		unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
		binding.queue.Retry(input)
		r.mu.Lock()
		binding.active = nil
		close(active.ready)
		close(active.done)
		r.mu.Unlock()
		active.cancel(nil)
		notification := taskQueueEvent(binding)
		unlock()
		r.loop.Emitter.Emit(notification)
	}()
	run, config, err := r.prepareNext(prepareCtx, input, identity.AgentID(input.SessionID), binding)
	if err != nil {
		if run != nil {
			binding.queue.Complete(input)
			saveErr := run.Settle(context.WithoutCancel(ctx), sessionentry.OutcomeFailed, sessionentry.RunFailureStorage, "")
			err = errors.Join(err, saveErr)
		}
		return err
	}
	unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
	binding.queue.Complete(input)
	r.mu.Lock()
	active.config = config
	err = r.launchLocked(run, active)
	r.mu.Unlock()
	notification := taskQueueEvent(binding)
	unlock()
	r.loop.Emitter.Emit(notification)
	return err
}

func (r *Coordinator) prepareNext(ctx context.Context, input taskmodel.Task, ownerID identity.AgentID, binding *sessionSchedule) (*agentrun.Run, *sessionmodel.Configuration, error) {
	config, model, err := binding.session.RunConfiguration()
	if err != nil {
		return nil, nil, err
	}
	definition, err := config.Definition(agentmodel.DefinitionPrimary)
	if err != nil {
		return nil, nil, err
	}
	run, err := agentrun.PrepareTask(ctx, r.runDependencies, input, ownerID, definition, model, binding.session)
	return run, config, err
}
