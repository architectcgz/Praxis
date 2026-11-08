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

// scheduleBinding 先在会话命令边界领取队首，再在锁外准备执行。
// 活动登记覆盖准备期，阻止会话在准备、执行和结算期间被释放或删除。
func (r *Coordinator) scheduleBinding(ctx context.Context, sessionID identity.SessionID, bindingID string, explicit bool) error {
	input, binding, active, err := r.claimTask(ctx, sessionID, bindingID, explicit)
	if err != nil || active == nil {
		return err
	}
	key := runKey{identity.AgentID(binding.sessionID), binding.branchID}
	return r.startNext(ctx, input, key, binding, active)
}

func (r *Coordinator) claimTask(ctx context.Context, sessionID identity.SessionID, bindingID string, explicit bool) (sessionmodel.Task, *sessionBinding, *activeRun, error) {
	unlock, err := r.lockSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.Task{}, nil, nil, err
	}
	defer unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	binding := r.bindings[bindingID]
	if r.closed || binding == nil || binding.sessionID != sessionID || binding.closing {
		return sessionmodel.Task{}, nil, nil, errCoordinatorClosed
	}
	key := runKey{identity.AgentID(binding.sessionID), binding.branchID}
	if active := r.runs[key]; active != nil {
		cause := context.Cause(active.ctx)
		if explicit && errors.Is(cause, errRunInterrupted) {
			active.scheduleRequested = true
		}
		return sessionmodel.Task{}, nil, nil, nil
	}
	input, ok := binding.queue.Next()
	if !ok {
		return sessionmodel.Task{}, nil, nil, nil
	}
	active := newActiveRun(ctx, binding.sessionID)
	active.branchID, active.bindingID = binding.branchID, bindingID
	r.runs[key] = active
	return input, binding, active, nil
}

func (r *Coordinator) startNext(ctx context.Context, input sessionmodel.Task, key runKey, binding *sessionBinding, active *activeRun) (err error) {
	prepareCtx, cancelPrepare := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(active.ctx, cancelPrepare)
	defer func() { stopCancellation(); cancelPrepare() }()
	defer func() {
		if err == nil {
			return
		}
		// 已接纳准备操作后，队首交接与清理不能再被调用方取消打断。
		unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
		r.mu.Lock()
		binding.queue.Retry(input)
		delete(r.runs, key)
		close(active.ready)
		active.cancel(nil)
		notification := taskQueueEvent(binding)
		r.mu.Unlock()
		unlock()
		r.loop.Emitter.Emit(notification)
	}()
	run, config, err := r.prepareNext(prepareCtx, input, key.agentID, binding)
	if err != nil {
		return err
	}
	runID := run.Snapshot().ID
	// 与入队去重串行交接，避免准备中的请求同时从历史查询和队列中消失。
	unlock, _ := r.lockSession(context.WithoutCancel(ctx), input.SessionID)
	r.mu.Lock()
	binding.queue.Complete(input)
	active.config = config
	err = r.launchLocked(run, runID, active)
	notification := taskQueueEvent(binding)
	r.mu.Unlock()
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
