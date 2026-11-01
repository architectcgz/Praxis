package coordinator

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

// scheduleBinding 在同一登记边界领取队首并交接 Session 所有权，模型和工具不持登记锁。
func (r *Coordinator) scheduleBinding(ctx context.Context, bindingID string, explicit bool) error {
	r.mu.Lock()
	binding := r.bindings[bindingID]
	if r.closed || binding == nil || binding.closing {
		r.mu.Unlock()
		return errCoordinatorClosed
	}
	key := executionKey{identity.AgentID(binding.sessionID), binding.branchID}
	if active := r.executions[key]; active != nil {
		cause := context.Cause(active.ctx)
		if explicit && (errors.Is(cause, errExecutionPaused) || errors.Is(cause, errExecutionStopped)) {
			active.resumeRequested = true
		}
		r.mu.Unlock()
		return nil
	}
	input, ok := binding.queue.Next()
	if !ok {
		r.mu.Unlock()
		return nil
	}
	active := newActiveExecution(ctx, binding.sessionID)
	active.branchID, active.bindingID = binding.branchID, bindingID
	r.executions[key] = active
	r.mu.Unlock()
	return r.startNext(ctx, input, key, binding, active)
}

func (r *Coordinator) startNext(ctx context.Context, input sessionmodel.Task, key executionKey, binding *sessionBinding, active *activeExecution) (err error) {
	prepareCtx, cancelPrepare := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(active.ctx, cancelPrepare)
	defer func() { stopCancellation(); cancelPrepare() }()
	defer func() {
		if err == nil {
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		binding.queue.Retry(input)
		delete(r.executions, key)
		close(active.ready)
		active.cancel(nil)
	}()
	run, config, err := r.prepareNext(prepareCtx, input, key.agentID, binding)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	binding.queue.Complete(input)
	active.config = config
	return r.launchLocked(run, active)
}
func (r *Coordinator) prepareNext(ctx context.Context, input sessionmodel.Task, ownerID identity.AgentID, binding *sessionBinding) (*agentexecution.Run, *sessionservice.Configuration, error) {
	config, model, err := r.sessions.ExecutionConfiguration(ctx, input.SessionID)
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
