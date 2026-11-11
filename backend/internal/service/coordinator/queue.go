package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

func (r *Coordinator) scheduleBinding(ctx context.Context, sessionID identity.SessionID, bindingID string, explicit bool) error {
	input, binding, active, err := r.claimTask(ctx, sessionID, bindingID, explicit)
	if err != nil || active == nil {
		return err
	}
	return r.startNext(ctx, input, runKey{identity.AgentID(sessionID), binding.branchID}, binding, active)
}

func (r *Coordinator) claimTask(ctx context.Context, sessionID identity.SessionID, bindingID string, explicit bool) (sessionmodel.Task, *sessionBinding, *activeRun, error) {
	unlock, err := r.lockSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.Task{}, nil, nil, err
	}
	defer unlock()
	return r.claimTaskLocked(ctx, sessionID, bindingID, explicit)
}

// claimTaskLocked 的调用方持有会话命令锁；注册表锁只用于定位和登记，不嵌套队列锁。
func (r *Coordinator) claimTaskLocked(ctx context.Context, sessionID identity.SessionID, bindingID string, explicit bool) (sessionmodel.Task, *sessionBinding, *activeRun, error) {
	r.mu.Lock()
	binding := r.bindings[sessionID]
	if r.closed || binding == nil || binding.id != bindingID || binding.closing {
		r.mu.Unlock()
		return sessionmodel.Task{}, nil, nil, errCoordinatorClosed
	}
	key := runKey{identity.AgentID(sessionID), binding.branchID}
	active := r.runs[key]
	r.mu.Unlock()
	if active != nil {
		if explicit && errors.Is(context.Cause(active.ctx), errRunInterrupted) {
			r.mu.Lock()
			active.scheduleRequested = true
			r.mu.Unlock()
		}
		return sessionmodel.Task{}, nil, nil, nil
	}
	input, ok := binding.queue.Next()
	if !ok {
		return sessionmodel.Task{}, nil, nil, nil
	}
	active = newActiveRun(ctx, sessionID)
	active.branchID, active.bindingID = binding.branchID, bindingID
	r.mu.Lock()
	r.runs[key] = active
	r.mu.Unlock()
	return input, binding, active, nil
}

func (r *Coordinator) startNext(ctx context.Context, input sessionmodel.Task, key runKey, binding *sessionBinding, active *activeRun) (err error) {
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
		delete(r.runs, key)
		close(active.ready)
		r.mu.Unlock()
		active.cancel(nil)
		notification := taskQueueEvent(binding)
		unlock()
		r.loop.Emitter.Emit(notification)
	}()
	run, config, err := r.prepareNext(prepareCtx, input, key.agentID, binding)
	if err != nil {
		return err
	}
	runID := run.Snapshot().ID
	unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
	binding.queue.Complete(input)
	r.mu.Lock()
	active.config = config
	err = r.launchLocked(run, runID, active)
	r.mu.Unlock()
	notification := taskQueueEvent(binding)
	unlock()
	r.loop.Emitter.Emit(notification)
	return err
}

func (r *Coordinator) prepareNext(ctx context.Context, input sessionmodel.Task, ownerID identity.AgentID, binding *sessionBinding) (*agentrun.Run, *sessionservice.Configuration, error) {
	config, model, err := r.sessions.RunConfiguration(ctx, input.SessionID)
	if err != nil {
		return nil, nil, err
	}
	definition, err := config.Definition(agentmodel.DefinitionPrimary)
	if err != nil {
		return nil, nil, err
	}
	run, _, err := r.operations.PrepareTask(ctx, input, ownerID, definition, model, binding.session, binding.id)
	return run, config, err
}
